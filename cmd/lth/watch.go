// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/mattdurham/lth/internal/compactor"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/mattdurham/lth/internal/watcher"
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

func init() {
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
		return nil                                     //nolint:nilerr // not running is not an error from stop's perspective
	}

	proc, findErr := os.FindProcess(pid)
	if findErr != nil || !isProcessAlive(pid) {
		_ = os.Remove(pidFile)
		fmt.Fprintln(os.Stderr, "daemon not running (stale pid file removed)") //nolint:errcheck
		return nil                                                               //nolint:nilerr // stale pid is not an error from stop's perspective
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

func runWatchDaemon(_ *cobra.Command, _ []string) error {
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

	// Create daemon components using internal packages directly.
	daemon, err := newDaemonComponents()
	if err != nil {
		return fmt.Errorf("create daemon components: %w", err)
	}
	defer daemon.close()

	// Start watcher goroutine.
	w, err := watcher.New(daemon.store, globalCfg)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	interval := time.Duration(globalCfg.Compaction.IntervalS) * time.Second

	errCh := make(chan error, 2)
	go func() { errCh <- w.Start(ctx) }()
	go func() { errCh <- daemon.compactor.Run(ctx, interval) }()

	slog.Info("daemon started", "pid", os.Getpid())

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
type daemonComponents struct {
	store     memory.Store
	ms        *memory.MemoryStore // concrete handle for Close (waits for async goroutines)
	compactor *compactor.Compactor
	d         *db.DB
}

func (dc *daemonComponents) close() {
	dc.ms.Close()  // wait for all scoreImportanceAsync goroutines before closing DB
	_ = dc.d.Close()
}

// newDaemonComponents creates the internal components for the daemon process.
func newDaemonComponents() (*daemonComponents, error) {
	dbPath := globalCfg.DB.Path
	d, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	emb := vector.NewOllamaEmbedder(globalCfg.Embedding.BaseURL, globalCfg.Embedding.Model, globalCfg.Embedding.TimeoutS)
	l := llm.NewOllamaLLM(globalCfg.LLM.BaseURL, globalCfg.LLM.Model, globalCfg.LLM.TimeoutS)
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
	}, nil
}
