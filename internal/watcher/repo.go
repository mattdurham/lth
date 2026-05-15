// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"os"
	"path/filepath"
	"strings"
)

// RepoForPath returns the Go module path (e.g. "github.com/mattdurham/lth") for
// a file path by finding the nearest git root and reading its go.mod.
// Returns "" if no git root or go.mod is found.
func RepoForPath(filePath string) string {
	root := findGitRoot(filepath.Dir(filePath))
	if root == "" {
		return ""
	}
	return readGoModule(filepath.Join(root, "go.mod"))
}

// findGitRoot walks up from dir until it finds a directory containing .git.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// readGoModule reads the module directive from a go.mod file.
func readGoModule(gomodPath string) string {
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if mod, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(mod)
		}
	}
	return ""
}
