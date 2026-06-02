// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mattdurham/lth/internal/compactor"
	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/issueswatcher"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
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

func runWatchDaemon(cmd *cobra.Command, _ []string) error {
	pidFile := pidFilePath(globalCfg)

	// Check if another daemon is already running.
	if pid, err := readPIDFile(pidFile); err == nil && isProcessAlive(pid) {
		slog.Info("daemon already running", "pid", pid)
		return nil
	}

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

	// Resolve metrics listen address from flag.
	metricsPort, err := cmd.Flags().GetInt("metrics-port")
	if err != nil {
		metricsPort = 10010
	}
	metricsAddr := fmt.Sprintf("localhost:%d", metricsPort)

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
	if globalCfg.Sync.ServerURL != "" {
		go autoSync(ctx, globalCfg, m)
	}
	if len(globalCfg.Markdown.Dirs) > 0 {
		mw := mdwatcher.New(daemon.ms, daemon.llm, globalCfg, m)
		go mw.Run(ctx)
	}
	if len(globalCfg.Issues.Repos) > 0 {
		iw := issueswatcher.New(daemon.ms, globalCfg, m)
		go iw.Run(ctx)
	}
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
