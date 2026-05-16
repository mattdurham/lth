// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package dataset

// repoLanguage maps known SWE-bench Multilingual Go repos to "go".
// The HuggingFace dataset has no "language" field; language is inferred from repo name.
var repoLanguage = map[string]string{
	"caddyserver/caddy":     "go",
	"gin-gonic/gin":         "go",
	"prometheus/prometheus": "go",
	"gohugoio/hugo":         "go",
	"hashicorp/terraform":   "go",
}

// Problem is one row from the SWE-bench Multilingual dataset.
type Problem struct {
	Repo             string   `json:"repo"`
	InstanceID       string   `json:"instance_id"`
	BaseCommit       string   `json:"base_commit"`
	Patch            string   `json:"patch"`
	TestPatch        string   `json:"test_patch"`
	ProblemStatement string   `json:"problem_statement"`
	HintsText        string   `json:"hints_text"`
	CreatedAt        string   `json:"created_at"`
	Version          string   `json:"version"`
	FailToPass       []string `json:"FAIL_TO_PASS"`
	PassToPass       []string `json:"PASS_TO_PASS"`
}

// Language returns the programming language for p.Repo, or "" if unknown.
func (p Problem) Language() string {
	return repoLanguage[p.Repo]
}
