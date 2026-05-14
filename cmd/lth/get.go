// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a memory by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func init() {
	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	id := args[0]

	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	m, err := client.Get(cmd.Context(), id)
	if err != nil {
		return fmt.Errorf("get %s: %w", id, err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	formatMemory(os.Stdout, m)
	return nil
}

// formatMemory prints a human-readable representation of a memory.
func formatMemory(w io.Writer, m *lth.Memory) {
	//nolint:errcheck // writing to stdout/stderr; errors are not actionable
	fmt.Fprintf(w, "ID:          %s\n", m.ID)
	fmt.Fprintf(w, "Layer:       %d\n", m.Layer)                                   //nolint:errcheck
	fmt.Fprintf(w, "Content:     %s\n", m.Content)                                 //nolint:errcheck
	fmt.Fprintf(w, "Importance:  %.1f\n", m.Importance)                            //nolint:errcheck
	fmt.Fprintf(w, "AccessCount: %d\n", m.AccessCount)                             //nolint:errcheck
	fmt.Fprintf(w, "CreatedAt:   %s\n", m.CreatedAt.Format("2006-01-02 15:04:05")) //nolint:errcheck
	fmt.Fprintf(w, "Source:      %s\n", m.Source)                                  //nolint:errcheck
	if len(m.Attrs) > 0 {
		fmt.Fprintln(w, "Attributes:") //nolint:errcheck
		for k, v := range m.Attrs {
			fmt.Fprintf(w, "  %s: %s\n", k, v) //nolint:errcheck
		}
	}
}
