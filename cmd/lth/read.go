// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var readTopK int

var readCmd = &cobra.Command{
	Use:   "read <file>",
	Short: "Read a file with lth memory context prepended as a header",
	Args:  cobra.ExactArgs(1),
	RunE:  runRead,
}

func init() {
	readCmd.Flags().IntVar(&readTopK, "top", 5, "number of lth memories to include in header")
	rootCmd.AddCommand(readCmd)
}

func runRead(cmd *cobra.Command, args []string) error {
	path := args[0]

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	// Search by parent/filename for specific hits, fall back to basename only.
	basename := filepath.Base(path)
	dirname := filepath.Base(filepath.Dir(path))

	query := basename
	if dirname != "." && dirname != "" {
		query = dirname + "/" + basename
	}

	results, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{3, 4, 5},
		TopK:   readTopK,
	})
	if err != nil {
		// Non-fatal: still return file content without header.
		results = nil
	}

	// Print header if we have memories.
	if len(results) > 0 {
		fmt.Fprintf(os.Stdout, "## lth context for `%s`\n\n", path) //nolint:errcheck
		for _, r := range results {
			fmt.Fprintf(os.Stdout, "%s\n\n> id: %s\n\n", r.Content, r.ID) //nolint:errcheck
		}
		fmt.Fprintf(os.Stdout, "---\n\n") //nolint:errcheck
	}

	// Print file content.
	fmt.Fprintf(os.Stdout, "## File: %s\n\n```\n%s\n```\n", path, content) //nolint:errcheck
	return nil
}
