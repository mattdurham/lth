// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	storeLayer  int
	storeAttrs  []string
	storeSource string
)

var storeCmd = &cobra.Command{
	Use:   "store <content>",
	Short: "Store a memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runStore,
}

func init() {
	storeCmd.Flags().IntVar(&storeLayer, "layer", 5, "memory layer (1-5, default 5)")
	storeCmd.Flags().StringArrayVar(&storeAttrs, "attr", nil, "attribute as key=val (repeatable)")
	storeCmd.Flags().StringVar(&storeSource, "source", "", "source identifier")
	rootCmd.AddCommand(storeCmd)
}

func runStore(cmd *cobra.Command, args []string) error {
	content := args[0]
	if storeLayer < 1 || storeLayer > 5 {
		return fmt.Errorf("layer must be 1-5, got %d", storeLayer)
	}

	attrs := parseAttrs(storeAttrs)
	if storeSource != "" {
		attrs["source"] = storeSource
	}

	// Auto-capture working directory and repo name unless already set.
	if _, ok := attrs["cwd"]; !ok {
		if cwd, err := os.Getwd(); err == nil {
			attrs["cwd"] = cwd
			if _, ok := attrs["repo"]; !ok {
				attrs["repo"] = filepath.Base(cwd)
			}
		}
	}

	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	m, err := client.Store(cmd.Context(), storeLayer, content, attrs)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	fmt.Println(m.ID)
	return nil
}

// parseAttrs parses key=val pairs into a map.
func parseAttrs(pairs []string) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
