// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import "github.com/spf13/cobra"

var (
	flagJSON    bool
	flagResults string
)

var rootCmd = &cobra.Command{
	Use:   "bench",
	Short: "bench — run SWE-bench problems through Claude approaches",
	Long:  "bench runs SWE-bench Multilingual Go problems sequentially through three Claude approaches and compares results.",
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "output results as JSON")
	rootCmd.PersistentFlags().StringVar(&flagResults, "results", "results.jsonl", "path to JSONL results file")
}
