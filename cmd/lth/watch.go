// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mattdurham/lth/internal/apiserver"
	"github.com/mattdurham/lth/internal/compactor"
	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/daemonlog"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/gwswatcher"
	"github.com/mattdurham/lth/internal/issueswatcher"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/mdwatcher"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/metrics"
	"github.com/mattdurham/lth/internal/traces"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/mattdurham/lth/internal/watcher"
	"github.com/mattdurham/lth/pkg/lth"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Manage the background daemon",
}

var watchStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background daemon (idempotent)",
	RunE:  runWatchStart,
}

var watchStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background daemon",
	RunE:  runWatchStop,
}

var watchDaemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run the background daemon (internal use)",
	Hidden: true,
	RunE:   runWatchDaemon,
}

var (
	flagUIPort int
	flagNoUI   bool
)

func init() {
	watchDaemonCmd.Flags().IntVar(&flagUIPort, "ui-port", 8765, "port for web UI")
	watchDaemonCmd.Flags().BoolVar(&flagNoUI, "no-ui", false, "disable the web UI")
	watchCmd.AddCommand(watchStartCmd, watchStopCmd, watchDaemonCmd)
	rootCmd.AddCommand(watchCmd)
}

func runWatchStart(_ *cobra.Command, _ []string) error {
	if err := ensureDaemon(globalCfg); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Println("daemon started")
	return nil
}

func runWatchStop(_ *cobra.Command, _ []string) error {
	pidFile := pidFilePath(globalCfg)
	pid, err := readPIDFile(pidFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "daemon not running") //nolint:errcheck
		return nil                                    //nolint:nilerr // not running is not an error from stop's perspective
	}

	proc, findErr := os.FindProcess(pid)
	if findErr != nil || !isProcessAlive(pid) {
		_ = os.Remove(pidFile)
		fmt.Fprintln(os.Stderr, "daemon not running (stale pid file removed)") //nolint:errcheck
		return nil                                                             //nolint:nilerr // stale pid is not an error from stop's perspective
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait up to 5s for PID file to disappear.
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			fmt.Println("daemon stopped")
			return nil
		}
	}
	return fmt.Errorf("daemon did not stop within 5 seconds")
}

// configReloadLoop polls path every interval. When the file's mtime changes, it
// attempts to re-load via config.ReloadInPlace. On a successful parse, hot fields
// (Compaction tuning, Search weights, Sync credentials, Markdown/Issues lists) are
// picked up by the running daemon on the next per-tick read. Fields that were
// captured at startup (DB path, embedder/LLM construction, fsnotify watch paths,
// ticker intervals) are logged but not applied — the daemon must be restarted to
// pick them up.
//
// A broken edit never kills the daemon: if the file fails to parse, the old
// config remains in place and the failure is logged at warn level.
func configReloadLoop(ctx context.Context, path string, cfg *config.Config, interval time.Duration) {
	if path == "" {
		slog.Debug("config reload disabled: no config path")
		return
	}
	var lastMtime time.Time
	// Seed lastMtime from current state so we don't fire a no-op reload on startup.
	if info, err := os.Stat(path); err == nil {
		lastMtime = info.ModTime()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			info, err := os.Stat(path)
			if err != nil {
				slog.Debug("config stat", "path", path, "err", err)
				continue
			}
			if !info.ModTime().After(lastMtime) {
				continue
			}
			changed, restart, err := config.ReloadInPlace(path, cfg)
			if err != nil {
				slog.Warn("config reload failed; keeping previous", "path", path, "err", err)
				// Do NOT advance lastMtime — retry next tick in case the user fixes the typo.
				continue
			}
			lastMtime = info.ModTime()
			if len(changed) == 0 {
				continue
			}
			slog.Info("config reloaded", "changed", changed, "requires_restart", restart)
		}
	}
}

// effectiveConfigPath returns the path the daemon is reading config from. Mirrors
// the resolution in root.go's PersistentPreRunE: --config flag overrides the
// default ~/.lth/config.yaml. Returns the empty string when neither is set.
func effectiveConfigPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	p, err := config.ConfigPath()
	if err != nil {
		return ""
	}
	return p
}

// walCheckpointLoop periodically runs PRAGMA wal_checkpoint(TRUNCATE) to keep the
// SQLite WAL file bounded. Without this the WAL can grow into the tens or hundreds
// of MB on a long-running daemon because SQLite's automatic checkpointer uses
// PASSIVE mode (reuses pages in place but never shrinks the file).
func walCheckpointLoop(ctx context.Context, d *db.DB, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			walPages, checkpointed, err := d.WALCheckpointTruncate(ctx)
			switch {
			case errors.Is(err, db.ErrCheckpointBusy):
				// Another connection held the WAL open. Data is safe; the next
				// tick (or a manual `lth maint checkpoint` with daemon stopped)
				// will shrink the WAL file. Logged at debug to avoid log spam.
				slog.Debug("wal checkpoint busy")
			case err != nil:
				slog.Debug("wal checkpoint", "wal_pages", walPages, "checkpointed", checkpointed, "err", err)
			case walPages > 0:
				slog.Debug("wal checkpoint", "wal_pages", walPages, "checkpointed", checkpointed)
			}
		}
	}
}

func runWatchDaemon(cmd *cobra.Command, _ []string) error {
	pidFile := pidFilePath(globalCfg)

	// Check if another daemon is already running.
	if pid, err := readPIDFile(pidFile); err == nil && isProcessAlive(pid) {
		slog.Info("daemon already running", "pid", pid)
		return nil
	}

	// Set up rotating log file. The parent (forkDaemon) opened daemon.log and
	// piped this child's stdout/stderr into it; we now take ownership via the
	// rotator, which dup2's its current fd over both std fds so any direct
	// writes (including panics) follow rotation. Any pre-rotator output
	// already went to the parent-supplied file, which is the same path.
	logPath := filepath.Join(filepath.Dir(globalCfg.DB.Path), "daemon.log")
	rotator, err := daemonlog.New(daemonlog.Options{
		Path:           logPath,
		RetainDays:     globalCfg.Watcher.LogRetainDays,
		RedirectStdFDs: true,
	})
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer rotator.Close() //nolint:errcheck
	level := slog.LevelInfo
	if flagVerbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(rotator, &slog.HandlerOptions{Level: level})))

	// Write PID file immediately.
	if err := writePIDFile(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidFile) //nolint:errcheck

	// Set up context with signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Create Prometheus registry and register all lth metrics.
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	// Resolve metrics listen address: config api.listen_addr takes precedence;
	// --metrics-port flag overrides the port portion only (for back-compat).
	metricsAddr := globalCfg.API.ListenAddr
	if metricsPort, err := cmd.Flags().GetInt("metrics-port"); err == nil {
		// Only override when the flag was explicitly set (non-default).
		if cmd.Flags().Changed("metrics-port") {
			host, _, _ := net.SplitHostPort(metricsAddr)
			metricsAddr = fmt.Sprintf("%s:%d", host, metricsPort)
		}
	}

	// Ensure embedding server (Docker) is running before creating components.
	if err := vector.EnsureEmbeddingServer(globalCfg); err != nil {
		slog.Warn("could not start embedding server", "err", err)
	}

	// Create daemon components, wrapping LLM and embedder with instrumentation.
	daemon, err := newDaemonComponents(m)
	if err != nil {
		return fmt.Errorf("create daemon components: %w", err)
	}
	defer daemon.close()

	// Start watcher goroutine.
	w, err := watcher.New(daemon.store, globalCfg, m)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	interval := time.Duration(globalCfg.Compaction.IntervalS) * time.Second

	// Start metrics HTTP server.
	metricsSrv := metrics.NewServer(metricsAddr, reg, daemon.store)

	// Optionally register the /api/v1/ REST API on the same port.
	if globalCfg.API.Enabled {
		apiClient, apiClientErr := lth.NewClient(globalCfg)
		if apiClientErr != nil {
			slog.Warn("REST API disabled: could not create client", "err", apiClientErr)
		} else {
			ah := apiserver.New(daemon.store, daemon.g, apiClient)
			metricsSrv.SetAPIHandler(ah)
			// apiClient is held open for the daemon lifetime; closed on daemon shutdown.
			defer apiClient.Close() //nolint:errcheck
			slog.Info("REST API enabled", "addr", "http://"+metricsAddr+"/api/v1/")
		}
	}

	go func() {
		if srvErr := metricsSrv.Start(ctx); srvErr != nil && srvErr != http.ErrServerClosed {
			slog.Error("metrics server error", "err", srvErr)
		}
	}()

	// Start metrics refresh loop (updates memory layer gauges every 30s).
	go metrics.RefreshLoop(ctx, daemon.store, m, 30*time.Second)

	// Start OTLP traces receiver.
	recv := traces.NewReceiver(daemon.store, daemon.g, daemon.d, slog.Default())
	metricsSrv.SetReceiver(recv)
	go recv.Start(ctx)

	errCh := make(chan error, 2)
	go func() { errCh <- w.Start(ctx) }()
	go func() { errCh <- daemon.compactor.Run(ctx, interval) }()
	go memory.BackfillValence(ctx, daemon.d, daemon.llm, 5, 10*time.Second)
	go memory.BackfillImportance(ctx, daemon.d, daemon.llm, 5, 15*time.Second)
	go memory.BackfillTags(ctx, daemon.d, daemon.llm, 5, 20*time.Second)
	go memory.BackfillEmbeddings(ctx, daemon.d, daemon.emb, config.EmbeddingModel, 50, 2*time.Second)
	go walCheckpointLoop(ctx, daemon.d, 5*time.Minute)
	go configReloadLoop(ctx, effectiveConfigPath(), globalCfg, 1*time.Minute)
	// Spawn all hot-reload-friendly watchers unconditionally. Each one self-gates
	// on its config block (Sync.ServerURL, Markdown.Dirs/.GitHub.Repos,
	// GWS.Enabled, Issues.Repos) and sleeps cheaply when disabled, so enabling
	// a watcher via config hot-reload takes effect on the next poll without a
	// daemon restart.
	go autoSync(ctx, globalCfg, m)
	// The mdwatcher itself includes globalCfg.GWS.OutputDir in its scan list
	// when GWS.Enabled, so we no longer mutate Markdown.Dirs at startup.
	// Mutating Markdown.Dirs here was reverted by the next config hot-reload,
	// which then triggered "file removed" soft-deletes on the gws-derived
	// memories.
	mw := mdwatcher.New(daemon.ms, daemon.llm, globalCfg, m)
	go mw.Run(ctx)
	gw := gwswatcher.New(globalCfg)
	go gw.Run(ctx)
	iw := issueswatcher.New(daemon.ms, globalCfg, m)
	go iw.Run(ctx)
	if !flagNoUI {
		uiClient, uiClientErr := lth.NewClient(globalCfg)
		if uiClientErr != nil {
			slog.Warn("web UI disabled: could not create client", "err", uiClientErr)
		} else {
			go func() {
				defer uiClient.Close() //nolint:errcheck
				startUIServer(ctx, uiClient, uiClient, flagUIPort)
			}()
			slog.Info("web UI running", "addr", fmt.Sprintf("http://localhost:%d", flagUIPort))
		}
	}

	slog.Info("daemon started", "pid", os.Getpid(), "metrics", metricsAddr)

	select {
	case <-ctx.Done():
		slog.Info("daemon shutting down")
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("daemon error: %w", err)
		}
		return nil
	}
}

// daemonComponents holds the internal components needed by the daemon.

// concrete handle for Close (waits for async goroutines)

func (dc *daemonComponents) close() {
	dc.ms.Close() // wait for all scoreImportanceAsync goroutines before closing DB
	_ = dc.d.Close()
}

// newDaemonComponents creates the internal components for the daemon process.
// m may be nil (metrics disabled); if non-nil, LLM and embedder are wrapped
// with instrumentation wrappers before being passed to the memory store.
func newDaemonComponents(m *metrics.Metrics) (*daemonComponents, error) {
	dbPath := globalCfg.DB.Path
	d, err := db.Open(dbPath, config.EmbeddingDim)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	emb := vector.NewEmbedder(globalCfg)
	l := llm.New(globalCfg)

	if m != nil {
		emb = metrics.NewInstrumentedEmbedder(emb, globalCfg.Embedding.Provider, m)
		l = metrics.NewInstrumentedLLM(l, globalCfg.LLM.Provider, m)
	}

	g := graph.New(d)

	ms, err := memory.NewMemoryStore(d, emb, l, g, globalCfg)
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("create memory store: %w", err)
	}

	c := compactor.New(ms, l, g, globalCfg, slog.Default())

	return &daemonComponents{
		store:     ms,
		ms:        ms,
		compactor: c,
		d:         d,
		g:         g,
		llm:       l,
		emb:       emb,
	}, nil
}
