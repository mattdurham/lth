// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/mattdurham/lth/internal/compactor"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/spf13/cobra"
)

var compactDryRun bool

var compactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Run memory compaction",
	RunE:  runCompact,
}

func init() {
	compactCmd.Flags().BoolVar(&compactDryRun, "dry-run", false, "show what would be compacted without making changes")
	rootCmd.AddCommand(compactCmd)
}

func runCompact(cmd *cobra.Command, _ []string) error {
	d, err := db.Open(globalCfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	emb := vector.NewOllamaEmbedder(globalCfg.Embedding.BaseURL, globalCfg.Embedding.Model, globalCfg.Embedding.TimeoutS)
	l := llm.NewOllamaLLM(globalCfg.LLM.BaseURL, globalCfg.LLM.Model, globalCfg.LLM.TimeoutS)
	g := graph.New(d)

	store, err := memory.NewMemoryStore(d, emb, l, g, globalCfg)
	if err != nil {
		return fmt.Errorf("create memory store: %w", err)
	}
	defer store.Close()

	if compactDryRun {
		// Dry run: just print what would happen.
		stats, err := store.Stats(cmd.Context())
		if err != nil {
			return fmt.Errorf("stats: %w", err)
		}
		fmt.Printf("Dry run — no changes made\n")
		fmt.Printf("L5 memories: %d (threshold: %d)\n", stats.ByLayer[5], globalCfg.Compaction.L5Threshold)
		fmt.Printf("L4 memories: %d\n", stats.ByLayer[4])
		fmt.Printf("L3 memories: %d\n", stats.ByLayer[3])
		return nil
	}

	c := compactor.New(store, l, g, globalCfg, slog.Default())
	report, err := c.RunOnce(cmd.Context())
	if err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(report)
	}

	fmt.Printf("Compacted: L5→L4: %d, L4→L3: %d, L3→L2: %d\n",
		report.L5toL4, report.L4toL3, report.L3toL2)
	if len(report.Errors) > 0 {
		for _, e := range report.Errors {
			fmt.Fprintf(os.Stderr, "error: %v\n", e)
		}
		return fmt.Errorf("%d compaction errors", len(report.Errors))
	}
	return nil
}
