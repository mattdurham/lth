// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tagAttrs []string

var tagCmd = &cobra.Command{
	Use:   "tag <id>",
	Short: "Add or update attributes on an existing memory",
	Long: `Add or update one or more attributes on an existing memory by ID.
Existing attributes not mentioned are left unchanged.

Example:
  lth tag abc123 --attr domain=coding
  lth tag abc123 --attr domain=coding --attr project=grafana/tempo`,
	Args: cobra.ExactArgs(1),
	RunE: runTag,
}

func init() {
	tagCmd.Flags().StringArrayVar(&tagAttrs, "attr", nil, "attribute as key=val (repeatable)")
	if err := tagCmd.MarkFlagRequired("attr"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(tagCmd)
}

func runTag(cmd *cobra.Command, args []string) error {
	id := args[0]

	attrs := parseAttrs(tagAttrs)
	if len(attrs) == 0 {
		return fmt.Errorf("at least one --attr key=value is required")
	}

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	for k, v := range attrs {
		if err := client.MergeAttr(cmd.Context(), id, k, v); err != nil {
			return fmt.Errorf("set %s=%s: %w", k, v, err)
		}
	}

	fmt.Printf("tagged %s: %v\n", id, tagAttrs)
	return nil
}
