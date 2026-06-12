// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/memory"
)

func makeCluster(n, contentLen int) []*memory.Memory {
	out := make([]*memory.Memory, n)
	pad := strings.Repeat("x", contentLen)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = &memory.Memory{
			ID:        "m" + string(rune('a'+i%26)),
			Content:   pad,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
	}
	return out
}

func TestSelectForPrompt_BelowBudget_ReturnsAll(t *testing.T) {
	c := makeCluster(10, 100) // 1000 chars total
	picked, total, sampled := selectForPrompt(c, 80_000)
	if sampled {
		t.Errorf("should not sample when below budget")
	}
	if len(picked) != 10 || total != 1000 {
		t.Errorf("got picked=%d total=%d", len(picked), total)
	}
}

func TestSelectForPrompt_OverBudget_DownsamplesEvenly(t *testing.T) {
	// 1000 memories × 1000 chars each = 1,000,000 chars; budget 80,000 → keep ~80
	c := makeCluster(1000, 1000)
	picked, total, sampled := selectForPrompt(c, 80_000)
	if !sampled {
		t.Fatalf("should sample: total=%d budget=80000", total)
	}
	if len(picked) < 50 || len(picked) > 100 {
		t.Errorf("expected ~80 picked, got %d", len(picked))
	}
	// Must include first and last to preserve time range.
	if picked[0] != c[0] {
		t.Error("sampling did not include first memory")
	}
	if picked[len(picked)-1] != c[len(c)-1] {
		t.Error("sampling did not include last memory")
	}
}

func TestSelectForPrompt_ZeroBudget_NoSampling(t *testing.T) {
	c := makeCluster(100, 10_000)
	picked, _, sampled := selectForPrompt(c, 0)
	if sampled || len(picked) != 100 {
		t.Errorf("zero budget should disable sampling: sampled=%v picked=%d", sampled, len(picked))
	}
}

func TestSelectForPrompt_PathologicalCase_AlwaysKeepsTwo(t *testing.T) {
	// 100 memories × 1MB each, budget 1KB -- proportional math gives 0 picks.
	// Floor must clamp to 2.
	c := makeCluster(100, 1_000_000)
	picked, _, sampled := selectForPrompt(c, 1000)
	if !sampled {
		t.Fatal("expected sampling")
	}
	if len(picked) < 2 {
		t.Errorf("got picked=%d, want >= 2", len(picked))
	}
}

func TestSelectForPrompt_OrderPreserved(t *testing.T) {
	c := makeCluster(500, 1000) // 500,000 chars total, budget 50,000 → ~50 picked
	picked, _, sampled := selectForPrompt(c, 50_000)
	if !sampled {
		t.Fatal("expected sampling")
	}
	for i := 1; i < len(picked); i++ {
		if !picked[i].CreatedAt.After(picked[i-1].CreatedAt) {
			t.Errorf("picked memories out of order at index %d", i)
		}
	}
}
