// Package gitproject detects the GitHub owner/repo for a directory from its git remote.
//
// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
package gitproject

import (
	"os/exec"
	"strings"
	"sync"
)

var cache sync.Map // dir → "owner/repo" or ""

// Detect returns "owner/repo" (e.g. "grafana/tempo") for the given directory,
// or "" if the directory is not in a git repo with a recognisable remote.
// Results are cached for the process lifetime.
func Detect(dir string) string {
	if v, ok := cache.Load(dir); ok {
		return v.(string)
	}
	p := detect(dir)
	cache.Store(dir, p)
	return p
}

func detect(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return parseRemote(strings.TrimSpace(string(out)))
}

// parseRemote extracts "owner/repo" from common Git remote URL formats:
//
//	https://github.com/owner/repo.git
//	https://github.com/owner/repo
//	git@github.com:owner/repo.git
func parseRemote(url string) string {
	// SSH: git@host:owner/repo.git
	if idx := strings.Index(url, ":"); idx != -1 && !strings.HasPrefix(url, "http") {
		path := url[idx+1:]
		return cleanRepo(path)
	}
	// HTTPS: https://host/owner/repo[.git]
	parts := strings.SplitN(url, "/", 5)
	if len(parts) >= 5 {
		return cleanRepo(parts[3] + "/" + parts[4])
	}
	return ""
}

func cleanRepo(s string) string {
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	// Must be "owner/repo" — reject anything with extra slashes
	if strings.Count(s, "/") != 1 {
		return ""
	}
	return s
}
