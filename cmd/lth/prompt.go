// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	promptTopEach int
	promptCWD     bool
)

var promptCmd = &cobra.Command{
	Use:   "prompt <query>",
	Short: "Generate a structured agent context block from memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runPrompt,
}

func init() {
	promptCmd.Flags().IntVar(&promptTopEach, "top-each", 5, "results per layer group")
	promptCmd.Flags().BoolVar(&promptCWD, "cwd", false, "filter L4/L5 results to memories where cwd matches current working directory")
	rootCmd.AddCommand(promptCmd)
}

func runPrompt(cmd *cobra.Command, args []string) error {
	query := args[0]

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	// L1+L2: Role & Principles
	principles, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{1, 2},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L1/L2: %w", err)
	}

	// L3: Relevant Techniques
	techniques, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{3},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L3: %w", err)
	}

	// L4+L5: Current Project Context
	context, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{4, 5},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L4/L5: %w", err)
	}

	// Optionally filter L4/L5 results by cwd attribute.
	if promptCWD {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}
		filtered := make([]*lth.SearchResult, 0, len(context))
		for _, r := range context {
			if r.Attrs["cwd"] == cwd {
				filtered = append(filtered, r)
			}
		}
		context = filtered
	}

	formatPromptOutput(os.Stdout, principles, techniques, context)
	return nil
}

// formatPromptOutput writes a structured markdown agent context block to w.
// Empty sections are omitted entirely.
func formatPromptOutput(w io.Writer, principles, techniques, context []*lth.SearchResult) {
	fmt.Fprintln(w, "# Agent Context") //nolint:errcheck

	if len(principles) > 0 {
		fmt.Fprintln(w, "\n## Role & Principles") //nolint:errcheck
		for _, r := range principles {
			fmt.Fprintf(w, "- %s\n", truncate(r.Content, 150)) //nolint:errcheck
		}
	}

	if len(techniques) > 0 {
		fmt.Fprintln(w, "\n## Relevant Techniques") //nolint:errcheck
		for _, r := range techniques {
			fmt.Fprintf(w, "- %s\n", truncate(r.Content, 150)) //nolint:errcheck
		}
	}

	if len(context) > 0 {
		fmt.Fprintln(w, "\n## Current Project Context") //nolint:errcheck
		for _, r := range context {
			fmt.Fprintf(w, "- %s\n", truncate(r.Content, 150)) //nolint:errcheck
		}
	}
}

// truncate shortens s to at most n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
