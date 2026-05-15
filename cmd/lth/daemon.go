// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

// ensureDaemon checks if the daemon is running; starts it if not.
func ensureDaemon(cfg *config.Config) error {
	pidFile := pidFilePath(cfg)

	pid, err := readPIDFile(pidFile)
	if err == nil && isProcessAlive(pid) {
		return nil // daemon already running
	}

	// Stale or missing PID file: start daemon.
	if err == nil {
		// PID file exists but process dead: remove stale file.
		_ = os.Remove(pidFile)
	}

	return forkDaemon(cfg)
}

// forkDaemon starts the daemon process and waits for the PID file to appear.
func forkDaemon(cfg *config.Config) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	logPath := filepath.Join(filepath.Dir(cfg.DB.Path), "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		logFile = nil
	}

	args := []string{"watch", "daemon"}
	if flagConfig != "" {
		args = append(args, "--config", flagConfig)
	}
	if flagDB != "" {
		args = append(args, "--db", flagDB)
	}

	cmd := newDaemonCmd(exe, args...)
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close() //nolint:errcheck
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Wait up to 1 second for PID file to appear.
	pidFile := pidFilePath(cfg)
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, statErr := os.Stat(pidFile); statErr == nil {
			return nil
		}
	}

	return errors.New("daemon did not start within 1 second")
}

// readPIDFile reads and parses a PID from the given file path.
func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}
	return pid, nil
}

// writePIDFile writes the given PID to the file at path.
func writePIDFile(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// isProcessAlive returns true if the process with the given PID is running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// pidFilePath returns the canonical path for the daemon PID file.
func pidFilePath(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.DB.Path), "watch.pid")
}
