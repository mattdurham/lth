// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List all projects tracked in the knowledge base",
	Long:  "Shows all distinct project values stored in memory attributes, with memory counts.",
	RunE:  runProjects,
}

func init() {
	rootCmd.AddCommand(projectsCmd)
}

func runProjects(cmd *cobra.Command, _ []string) error {
	client, err := newClientFromGlobalCfg()
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	projects, err := client.DistinctAttrValues(cmd.Context(), "project")
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		fmt.Println("Projects are tagged automatically when the watcher or markdown ingester")
		fmt.Println("detects a git remote (e.g. grafana/tempo) in the working directory.")
		return nil
	}

	fmt.Printf("%-40s  %s\n", "Project", "Filter usage")
	fmt.Printf("%-40s  %s\n", "-------", "------------")
	for _, p := range projects {
		fmt.Printf("%-40s  lth prompt \"...\" --attr project=%s\n", p, p)
	}
	fmt.Println()
	fmt.Println("Use --attr to boost memories from a specific project:")
	fmt.Println("  lth prompt \"tempo livestore mutex\" --attr project=grafana/tempo")
	fmt.Println("  lth chat \"what did we fix in tempo?\" --attr project=grafana/tempo")
	return nil
}
