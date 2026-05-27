// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package dataset

import (
	"encoding/json"
	"testing"
)

func TestLanguageKnownRepos(t *testing.T) {
	known := []string{
		"caddyserver/caddy",
		"gin-gonic/gin",
		"prometheus/prometheus",
		"gohugoio/hugo",
		"hashicorp/terraform",
	}
	for _, repo := range known {
		t.Run(repo, func(t *testing.T) {
			p := Problem{Repo: repo}
			if got := p.Language(); got != "go" {
				t.Errorf("Language() = %q, want \"go\"", got)
			}
		})
	}
}

func TestLanguageUnknownRepo(t *testing.T) {
	p := Problem{Repo: "python/cpython"}
	if got := p.Language(); got != "" {
		t.Errorf("Language() = %q, want \"\"", got)
	}
}

func TestFailToPassUnmarshal(t *testing.T) {
	raw := `{
		"repo": "gin-gonic/gin",
		"instance_id": "gin-gonic__gin-1234",
		"base_commit": "abc123",
		"patch": "",
		"test_patch": "",
		"problem_statement": "fix bug",
		"hints_text": "",
		"created_at": "2024-01-01",
		"version": "1.0",
		"FAIL_TO_PASS": ["TestFoo", "TestBar"],
		"PASS_TO_PASS": ["TestBaz"]
	}`
	var p Problem
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.FailToPass) != 2 {
		t.Errorf("FailToPass len = %d, want 2", len(p.FailToPass))
	}
	if p.FailToPass[0] != "TestFoo" {
		t.Errorf("FailToPass[0] = %q, want \"TestFoo\"", p.FailToPass[0])
	}
	if p.FailToPass[1] != "TestBar" {
		t.Errorf("FailToPass[1] = %q, want \"TestBar\"", p.FailToPass[1])
	}
	if len(p.PassToPass) != 1 {
		t.Errorf("PassToPass len = %d, want 1", len(p.PassToPass))
	}
}
