// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package report

import (
	"fmt"
	"io"

	"github.com/mattdurham/lth/internal/bench/runner"
)

type approachStats struct {
	problems int
	patches  int
	noPatch  int
	failed   int
}

// PrintSummary writes a formatted summary table to w.
// Groups results by approach. PatchRate = patches generated / problems attempted.
func PrintSummary(results []runner.Result, w io.Writer) {
	if len(results) == 0 {
		fmt.Fprintln(w, "no results")
		return
	}

	order := []string{}
	seen := map[string]bool{}
	stats := map[string]*approachStats{}

	for _, r := range results {
		if !seen[r.Approach] {
			seen[r.Approach] = true
			order = append(order, r.Approach)
			stats[r.Approach] = &approachStats{}
		}
		s := stats[r.Approach]
		s.problems++
		switch r.Outcome {
		case runner.OutcomePass:
			s.patches++
		case runner.OutcomeNoPatch:
			s.noPatch++
		default:
			s.failed++
		}
	}

	fmt.Fprintf(w, "%-12s  %8s  %7s  %7s  %6s  %9s\n",
		"Approach", "Problems", "Patches", "NoPatch", "Failed", "PatchRate")
	fmt.Fprintf(w, "%-12s  %8s  %7s  %7s  %6s  %9s\n",
		"-----------", "--------", "-------", "-------", "------", "---------")

	for _, approach := range order {
		s := stats[approach]
		var rate float64
		if s.problems > 0 {
			rate = float64(s.patches) / float64(s.problems) * 100
		}
		fmt.Fprintf(w, "%-12s  %8d  %7d  %7d  %6d  %8.1f%%\n",
			approach, s.problems, s.patches, s.noPatch, s.failed, rate)
	}
}
