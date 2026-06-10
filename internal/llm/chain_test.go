// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"errors"
	"fmt"
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
