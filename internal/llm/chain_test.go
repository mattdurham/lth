// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubLLM struct {
	name    string
	out     string
	err     error
	calls   int64
	delay   time.Duration
	onCall  func()
}

func (s *stubLLM) Complete(ctx context.Context, _ string) (string, error) {
	atomic.AddInt64(&s.calls, 1)
	if s.onCall != nil {
		s.onCall()
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.out, s.err
}

func TestChain_PrimarySucceeds(t *testing.T) {
	prim := &stubLLM{name: "p", out: "ok"}
	fb := &stubLLM{name: "f", out: "no", err: errors.New("should not run")}
	c := NewChain(ChainConfig{}, ChainEntry{Name: "p", LLM: prim}, ChainEntry{Name: "f", LLM: fb})
	got, err := c.Complete(context.Background(), "hi")
	if err != nil || got != "ok" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if atomic.LoadInt64(&fb.calls) != 0 {
		t.Errorf("fallback was called: %d", fb.calls)
	}
}

func TestChain_FallsBackOnError(t *testing.T) {
	prim := &stubLLM{name: "p", err: errors.New("connection refused")}
	fb := &stubLLM{name: "f", out: "ok"}
	c := NewChain(ChainConfig{}, ChainEntry{Name: "p", LLM: prim}, ChainEntry{Name: "f", LLM: fb})
	got, err := c.Complete(context.Background(), "hi")
	if err != nil || got != "ok" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if atomic.LoadInt64(&prim.calls) != 1 || atomic.LoadInt64(&fb.calls) != 1 {
		t.Errorf("call counts: primary=%d fallback=%d", prim.calls, fb.calls)
	}
}

func TestChain_FallsBackOnEmptyContent(t *testing.T) {
	prim := &stubLLM{name: "p", out: "   "} // whitespace -> treated as empty
	fb := &stubLLM{name: "f", out: "real answer"}
	c := NewChain(ChainConfig{}, ChainEntry{Name: "p", LLM: prim}, ChainEntry{Name: "f", LLM: fb})
	got, err := c.Complete(context.Background(), "hi")
	if err != nil || got != "real answer" {
		t.Fatalf("got (%q, %v)", got, err)
	}
}

func TestChain_AllFail(t *testing.T) {
	prim := &stubLLM{err: errors.New("primary down")}
	fb := &stubLLM{err: errors.New("fallback down")}
	c := NewChain(ChainConfig{}, ChainEntry{Name: "p", LLM: prim}, ChainEntry{Name: "f", LLM: fb})
	_, err := c.Complete(context.Background(), "hi")
	if err == nil || !errors.Is(err, prim.err) {
		t.Fatalf("expected wrapped primary error, got %v", err)
	}
}

func TestChain_CallerContextCanceled_NoFallback(t *testing.T) {
	prim := &stubLLM{err: context.Canceled}
	fb := &stubLLM{out: "should-not-run"}
	c := NewChain(ChainConfig{}, ChainEntry{Name: "p", LLM: prim}, ChainEntry{Name: "f", LLM: fb})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Complete(ctx, "hi"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if atomic.LoadInt64(&fb.calls) != 0 {
		t.Errorf("fallback ran despite caller cancel: %d", fb.calls)
	}
}

func TestChain_PerBackendTimeout(t *testing.T) {
	slow := &stubLLM{out: "slow", delay: 200 * time.Millisecond}
	fast := &stubLLM{out: "fast"}
	c := NewChain(ChainConfig{},
		ChainEntry{Name: "slow", LLM: slow, Timeout: 50 * time.Millisecond},
		ChainEntry{Name: "fast", LLM: fast},
	)
	start := time.Now()
	got, err := c.Complete(context.Background(), "hi")
	elapsed := time.Since(start)
	if err != nil || got != "fast" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("took too long (%v); per-backend timeout did not fire", elapsed)
	}
}

func TestCircuitBreaker_OpensAndCoolsDown(t *testing.T) {
	cb := newCircuitBreaker(4, 0.5, 50*time.Millisecond)
	for range 4 {
		cb.recordFailure()
	}
	if cb.allowRequest() {
		t.Fatal("breaker should be open after 4/4 failures")
	}
	time.Sleep(60 * time.Millisecond)
	if !cb.allowRequest() {
		t.Fatal("breaker should half-open after cooldown")
	}
}

func TestChain_CircuitBreakerSkipsDeadBackend(t *testing.T) {
	deadCalls := int64(0)
	dead := &stubLLM{err: errors.New("connection refused"), onCall: func() { atomic.AddInt64(&deadCalls, 1) }}
	live := &stubLLM{out: "ok"}
	c := NewChain(ChainConfig{
		CircuitWindow:     3,
		CircuitFailurePct: 0.5,
		CircuitCooldown:   30 * time.Second,
	},
		ChainEntry{Name: "dead", LLM: dead},
		ChainEntry{Name: "live", LLM: live},
	)
	// Trip the breaker: 3 failures fill the window with 100% failure rate.
	for i := range 3 {
		if _, err := c.Complete(context.Background(), "hi"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if atomic.LoadInt64(&deadCalls) != 3 {
		t.Fatalf("dead calls = %d, want 3", deadCalls)
	}
	// Next 10 calls should skip the dead backend entirely.
	for range 10 {
		if _, err := c.Complete(context.Background(), "hi"); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt64(&deadCalls); got != 3 {
		t.Errorf("circuit did not block subsequent calls: deadCalls=%d, want 3", got)
	}
}

// TestChain_SemaphoreSerializes verifies that a MaxConcurrent=1 backend
// processes calls strictly one at a time. We fire N concurrent Complete
// calls and observe that the stub's in-flight count never exceeds 1.
func TestChain_SemaphoreSerializes(t *testing.T) {
	var inFlight, maxObserved int64
	slow := &stubLLM{out: "ok", delay: 30 * time.Millisecond, onCall: func() {
		n := atomic.AddInt64(&inFlight, 1)
		defer atomic.AddInt64(&inFlight, -1)
		for {
			old := atomic.LoadInt64(&maxObserved)
			if n <= old || atomic.CompareAndSwapInt64(&maxObserved, old, n) {
				break
			}
		}
	}}
	c := NewChain(ChainConfig{},
		ChainEntry{Name: "primary", LLM: slow, Timeout: 5 * time.Second, MaxConcurrent: 1},
	)

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Complete(context.Background(), "hi"); err != nil {
				t.Errorf("Complete: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&maxObserved); got > 1 {
		t.Errorf("max in-flight = %d, want <= 1 (semaphore not serialising)", got)
	}
	if got := atomic.LoadInt64(&slow.calls); got != 6 {
		t.Errorf("slow.calls = %d, want 6", got)
	}
}

// TestChain_SemaphoreSaturationFallsOver: when the primary semaphore is
// already full and the per-backend timeout expires while the caller is in
// the queue, the chain should fall through to the next backend instead of
// blocking forever.
func TestChain_SemaphoreSaturationFallsOver(t *testing.T) {
	holdReleased := make(chan struct{})
	primary := &stubLLM{out: "primary-ok", onCall: func() {
		<-holdReleased // first call holds the sem until released
	}}
	fallback := &stubLLM{out: "fallback-ok"}

	c := NewChain(ChainConfig{},
		ChainEntry{Name: "primary", LLM: primary, Timeout: 100 * time.Millisecond, MaxConcurrent: 1},
		ChainEntry{Name: "fallback", LLM: fallback, Timeout: 5 * time.Second},
	)

	// Kick off a holder that will occupy the semaphore.
	go func() {
		_, _ = c.Complete(context.Background(), "holder")
	}()
	// Give the holder time to acquire the sem and start its (blocking) call.
	time.Sleep(30 * time.Millisecond)

	// Second call must wait for the sem, time out, and fall over to fallback.
	start := time.Now()
	got, err := c.Complete(context.Background(), "queued")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "fallback-ok" {
		t.Errorf("got %q, want fallback to handle it", got)
	}
	// Should fall over close to the primary timeout (100ms), not block until holder releases.
	if elapsed > 250*time.Millisecond {
		t.Errorf("fallback took %v; sem wait did not respect per-backend timeout", elapsed)
	}

	// Release the holder so it can exit cleanly.
	close(holdReleased)
}

// TestChain_TimeoutLookupOverrides verifies that SetTimeoutLookup's return
// value, when non-zero, overrides the static ChainEntry.Timeout on every
// call. Enables hot-reloading timeouts from the live config without
// rebuilding the chain.
func TestChain_TimeoutLookupOverrides(t *testing.T) {
	slow := &stubLLM{out: "primary-ok", delay: 200 * time.Millisecond}
	fallback := &stubLLM{out: "fallback-ok"}

	c := NewChain(ChainConfig{},
		ChainEntry{Name: "primary", LLM: slow, Timeout: 5 * time.Second},
		ChainEntry{Name: "fallback", LLM: fallback, Timeout: 5 * time.Second},
	)

	// Override primary timeout to 50ms via lookup -- primary will be killed
	// mid-call by its 200ms delay vs 50ms timeout. Fallback gets a normal
	// 5s timeout (lookup returns 0 for index 1 -> static used).
	c.SetTimeoutLookup(func(i int) time.Duration {
		if i == 0 {
			return 50 * time.Millisecond
		}
		return 0 // use static
	})

	start := time.Now()
	got, err := c.Complete(context.Background(), "hi")
	elapsed := time.Since(start)
	if err != nil || got != "fallback-ok" {
		t.Fatalf("got (%q, %v); want fallback to take over after primary times out", got, err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("failover took %v; timeoutLookup not honoured", elapsed)
	}
}

func TestChain_FastFailover_TimingBudget(t *testing.T) {
	// Demonstrates the headline performance claim: a dead primary with a tight
	// timeout falls over to a healthy backup well under one "primary timeout"
	// of wall clock.
	dead := &stubLLM{err: errors.New("connection refused")}
	live := &stubLLM{out: "ok"}
	c := NewChain(ChainConfig{},
		ChainEntry{Name: "dead", LLM: dead, Timeout: 100 * time.Millisecond},
		ChainEntry{Name: "live", LLM: live, Timeout: 100 * time.Millisecond},
	)
	start := time.Now()
	got, err := c.Complete(context.Background(), "hi")
	elapsed := time.Since(start)
	if err != nil || got != "ok" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("failover took %v; should be near-instant for connection-refused", elapsed)
	}
	_ = fmt.Sprint(elapsed) // keep import
}
