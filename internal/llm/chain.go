// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ChainConfig configures the fallback chain behavior.
type ChainConfig struct {
	// CircuitWindow is the rolling window size for failure tracking. Default: 10.
	CircuitWindow int
	// CircuitFailurePct is the fraction (0.0-1.0) of failures in the window
	// that opens the breaker. Default: 0.5.
	CircuitFailurePct float64
	// CircuitCooldown is how long to skip a backend with an open breaker
	// before probing it again. Default: 30s.
	CircuitCooldown time.Duration
}

func (c *ChainConfig) applyDefaults() {
	if c.CircuitWindow <= 0 {
		c.CircuitWindow = 10
	}
	if c.CircuitFailurePct <= 0 || c.CircuitFailurePct > 1 {
		c.CircuitFailurePct = 0.5
	}
	if c.CircuitCooldown <= 0 {
		c.CircuitCooldown = 30 * time.Second
	}
}

// ChainEntry is one backend in a fallback chain.
type ChainEntry struct {
	Name    string        // for logging (e.g. "primary:openai", "fallback:anthropic")
	LLM     LLM           // the underlying client
	Timeout time.Duration // per-request timeout (0 = no extra timeout beyond caller ctx)
	// MaxConcurrent caps the number of in-flight Complete calls to this
	// backend. Calls beyond the limit wait on a Go-channel semaphore (bounded
	// by the per-backend Timeout) before the underlying client is invoked.
	// 0 (default) means unbounded -- behaviour identical to the pre-semaphore
	// chain. Use 1 for a serial local model so concurrent backfill workers
	// don't pile up inside the inference server's own queue.
	MaxConcurrent int
}

// Chain wraps an ordered list of LLM backends. Complete tries each in order
// until one returns a non-fallback error or succeeds.
type Chain struct {
	entries []chainSlot
	cfg     ChainConfig
}

type chainSlot struct {
	ChainEntry
	breaker *circuitBreaker
	sem     chan struct{} // nil = unbounded; otherwise cap == MaxConcurrent
}

// NewChain constructs a fallback chain from an ordered list of backends.
// The first entry is the primary.
func NewChain(cfg ChainConfig, entries ...ChainEntry) *Chain {
	cfg.applyDefaults()
	slots := make([]chainSlot, len(entries))
	for i, e := range entries {
		var sem chan struct{}
		if e.MaxConcurrent > 0 {
			sem = make(chan struct{}, e.MaxConcurrent)
		}
		slots[i] = chainSlot{
			ChainEntry: e,
			breaker:    newCircuitBreaker(cfg.CircuitWindow, cfg.CircuitFailurePct, cfg.CircuitCooldown),
			sem:        sem,
		}
	}
	return &Chain{entries: slots, cfg: cfg}
}

// Compile-time interface check.
var _ LLM = (*Chain)(nil)

// Complete tries each backend in order until one succeeds or all fail with
// fallback-worthy errors. Returns the first error if all backends fail.
func (c *Chain) Complete(ctx context.Context, prompt string) (string, error) {
	if len(c.entries) == 0 {
		return "", errors.New("llm chain: no backends configured")
	}

	var firstErr error
	tried := 0

	for i, slot := range c.entries {
		// Skip backends whose circuit is open and still cooling down.
		if !slot.breaker.allowRequest() {
			slog.Debug("llm chain: skipping backend (circuit open)", "name", slot.Name)
			continue
		}

		// Honour caller cancellation before each attempt.
		if err := ctx.Err(); err != nil {
			return "", err
		}

		callCtx := ctx
		var cancel context.CancelFunc
		if slot.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, slot.Timeout)
		}

		// Admission control: if this backend is concurrency-limited, acquire a
		// slot in the semaphore before calling the underlying client. The wait
		// is bounded by callCtx, so a long queue eventually falls over to the
		// next backend rather than blocking indefinitely.
		if slot.sem != nil {
			queueStart := time.Now()
			select {
			case slot.sem <- struct{}{}:
				if wait := time.Since(queueStart); wait > 50*time.Millisecond {
					slog.Debug("llm chain: waited on backend semaphore",
						"name", slot.Name, "wait_ms", wait.Milliseconds())
				}
			case <-callCtx.Done():
				if cancel != nil {
					cancel()
				}
				// Treat queue starvation as a non-fatal failure: don't count it
				// against the circuit breaker (it's congestion, not a backend
				// fault) but do fall through to the next backend so the caller
				// isn't blocked.
				slog.Debug("llm chain: backend queue saturated, falling back",
					"name", slot.Name, "elapsed_ms", time.Since(queueStart).Milliseconds())
				if firstErr == nil {
					firstErr = fmt.Errorf("backend %s: queue saturated: %w", slot.Name, callCtx.Err())
				}
				tried++
				continue
			}
		}

		start := time.Now()
		out, err := slot.LLM.Complete(callCtx, prompt)
		if slot.sem != nil {
			<-slot.sem
		}
		if cancel != nil {
			cancel()
		}
		tried++

		// Treat empty content as a soft failure (reasoning models occasionally
		// return "" when they exceed token budget while still in <think>).
		if err == nil && strings.TrimSpace(out) == "" {
			err = errEmptyContent
		}

		if err == nil {
			slot.breaker.recordSuccess()
			if i > 0 {
				slog.Info("llm chain: succeeded on fallback",
					"name", slot.Name, "skipped", i, "elapsed_ms", time.Since(start).Milliseconds())
			}
			return out, nil
		}

		// Caller context death is final; do not fall back.
		if isContextCancel(ctx, err) {
			return "", err
		}

		slot.breaker.recordFailure()

		if firstErr == nil {
			firstErr = err
		}

		if !shouldFallback(err) {
			slog.Warn("llm chain: non-fallback error", "name", slot.Name, "err", err)
			return "", err
		}

		slog.Warn("llm chain: backend failed, falling back",
			"name", slot.Name, "err", err, "elapsed_ms", time.Since(start).Milliseconds())
	}

	if tried == 0 {
		return "", errors.New("llm chain: all backends in circuit-open state")
	}
	return "", fmt.Errorf("llm chain: all backends failed (last error): %w", firstErr)
}

// AnthropicEntry returns the first chain entry whose LLM is an *AnthropicLLM,
// or nil if none. Used by callers that need CompleteWithTools, which is only
// implemented on AnthropicLLM.
func (c *Chain) AnthropicEntry() *AnthropicLLM {
	for _, slot := range c.entries {
		if a, ok := slot.LLM.(*AnthropicLLM); ok {
			return a
		}
	}
	return nil
}

// errEmptyContent marks an empty-string response as a fallback-worthy error.
var errEmptyContent = errors.New("llm: empty content (likely reasoning-model bleed)")

// isContextCancel reports whether the error is the caller's ctx being
// cancelled (not a per-backend timeout we created).
func isContextCancel(callerCtx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if callerCtx.Err() != nil {
		return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}
	return false
}

// shouldFallback classifies an error as transient (try next backend) or
// permanent (return immediately). Transient: connection errors, timeouts,
// 5xx, 429, empty content. We are deliberately permissive: 4xx on the
// primary may still succeed on a different backend with a different schema.
func shouldFallback(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errEmptyContent) {
		return true
	}
	// All non-cancel errors are treated as transient. Cancel is filtered out
	// above by isContextCancel.
	return true
}

// circuitBreaker tracks a rolling window of successes/failures per backend.
// When the failure fraction exceeds a threshold, the breaker opens and
// rejects requests until a cooldown elapses, then half-opens (allows one
// probe).
type circuitBreaker struct {
	mu        sync.Mutex
	window    []bool // true = success, false = failure
	idx       int
	threshold float64
	cooldown  time.Duration
	openedAt  time.Time
}

func newCircuitBreaker(window int, failurePct float64, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{
		window:    make([]bool, 0, window),
		threshold: failurePct,
		cooldown:  cooldown,
	}
}

// allowRequest reports whether the breaker permits a request through.
// In half-open state (cooldown elapsed), it allows a probe by returning true
// once; subsequent recordSuccess/recordFailure adjusts state.
func (cb *circuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.openedAt.IsZero() {
		return true
	}
	if time.Since(cb.openedAt) >= cb.cooldown {
		// Half-open: clear the open state and allow one probe.
		cb.openedAt = time.Time{}
		cb.window = cb.window[:0]
		cb.idx = 0
		return true
	}
	return false
}

func (cb *circuitBreaker) recordSuccess() { cb.record(true) }
func (cb *circuitBreaker) recordFailure() { cb.record(false) }

func (cb *circuitBreaker) record(ok bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if len(cb.window) < cap(cb.window) {
		cb.window = append(cb.window, ok)
	} else {
		cb.window[cb.idx] = ok
		cb.idx = (cb.idx + 1) % cap(cb.window)
	}

	// Only consider opening once we have a full window.
	if len(cb.window) < cap(cb.window) {
		return
	}
	fails := 0
	for _, v := range cb.window {
		if !v {
			fails++
		}
	}
	if float64(fails)/float64(len(cb.window)) >= cb.threshold {
		cb.openedAt = time.Now()
	}
}
