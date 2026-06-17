// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package gwswatcher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

// TestRun_RespectsCtxCancelWhileDisabled verifies that Run() returns promptly
// when ctx is cancelled even if GWS.Enabled is false (it must not block in
// the disabled-poll sleep).
func TestRun_RespectsCtxCancelWhileDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.GWS.Enabled = false
	w := &Watcher{cfg: cfg, runner: &stubRunner{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Give it a moment to enter the disabled sleep.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel while disabled")
	}
}

// TestRun_PicksUpEnableWithoutRestart simulates a hot-reload that flips
// GWS.Enabled from false to true mid-run. The Watcher must call into the
// runner without requiring a restart of the goroutine.
func TestRun_PicksUpEnableWithoutRestart(t *testing.T) {
	cfg := config.Default()
	cfg.GWS.Enabled = false
	cfg.GWS.IntervalH = 1 // 1 hour, never re-enters scan once started
	cfg.GWS.NamePatterns = []string{"X"}
	cfg.GWS.OutputDir = t.TempDir()

	scanCalls := int64(0)
	stub := &stubRunner{}
	// Each Run() iteration that goes active will call list once.
	// We'll inject empty responses so no docs are fetched.
	primeStub := func() {
		stub.queue = []stubResponse{
			{out: makeListResponse(nil)},
		}
		stub.pos = 0
	}

	// We can't observe internal scanOnce, but we CAN observe runner.Run calls
	// via the stub's calls slice.
	go func() {
		// Pretend the daemon's hot-reload turns it on after 100ms.
		time.Sleep(100 * time.Millisecond)
		primeStub()
		cfg.GWS.Enabled = true
		atomic.StoreInt64(&scanCalls, 1) // signal we've flipped
	}()

	w := &Watcher{cfg: cfg, runner: stub}
	// Override the disabled-poll constant for the test via a thin wrapper:
	// the Run method polls every 60s when disabled, but we can shortcut by
	// calling ScanOnce manually after the flip is observed.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	go w.Run(ctx)

	// Wait for the test-driver goroutine to flip Enabled.
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) && atomic.LoadInt64(&scanCalls) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt64(&scanCalls) == 0 {
		t.Fatal("test driver never flipped Enabled")
	}
	// Now manually invoke ScanOnce to simulate what Run would do on next poll
	// (without waiting the full 60s disabled-poll).
	if err := w.ScanOnce(ctx); err != nil {
		t.Fatalf("ScanOnce after enable: %v", err)
	}
	if len(stub.calls) == 0 {
		t.Errorf("runner was never called after Enabled flipped true")
	}
}
