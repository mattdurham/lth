// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package mdwatcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests use generic fixture repo names only. No real public or private GitHub
// repository should appear in this file by policy.

func TestValidRepoSpec(t *testing.T) {
	cases := map[string]bool{
		"acme/widgets":         true,
		"acme/widget_tools":    true,
		"example-org/repo-one": true,
		"":                     false,
		"acme":                 false,
		"/widgets":             false,
		"acme/":                false,
		"acme/widgets/extra":   false,
		"../etc/passwd":        false,
		"acme/.git":            false,
		"acme corp/widgets":    false,
	}
	for spec, want := range cases {
		if got := validRepoSpec(spec); got != want {
			t.Errorf("validRepoSpec(%q) = %v, want %v", spec, got, want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		glob string
		path string
		want bool
	}{
		// ** matches any number of segments (including zero)
		{"**/foo/**", "foo/deploy.yaml", true},
		{"**/foo/**", "a/b/foo/c/d.yaml", true},
		{"**/foo/**", "a/foo", true},
		{"**/foo/**", "a/b/c.md", false},
		{"**/foo/**", "foolite/x.md", false},

		// **/x.md = file named x.md anywhere
		{"**/x.md", "x.md", true},
		{"**/x.md", "a/x.md", true},
		{"**/x.md", "a/b/c/x.md", true},
		{"**/x.md", "a/xy.md", false},

		// Trailing /**
		{"docs/**", "docs/a/b.md", true},
		{"docs/**", "docs", true},
		{"docs/**", "doc/a.md", false},

		// Leading **
		{"**/README.md", "README.md", true},
		{"**/README.md", "a/b/README.md", true},

		// Specific multi-segment subtree
		{"a/b/c/**", "a/b/c/foo.yaml", true},
		{"a/b/c/**", "a/b/c", true},
		{"a/b/c/**", "a/b/d/foo.yaml", false},

		// Single-segment glob: * does NOT cross /
		{"docs/*.md", "docs/intro.md", true},
		{"docs/*.md", "docs/sub/intro.md", false},

		// ? = single char
		{"docs/a?.md", "docs/ab.md", true},
		{"docs/a?.md", "docs/abc.md", false},

		// Edge: empty path with **
		{"**", "anything/here", true},
		{"**", "", true},

		// Multiple **: collapsed
		{"**/**/x.md", "a/b/x.md", true},
		{"**/**/x.md", "x.md", true},
	}
	for _, c := range cases {
		got := globMatch(c.glob, c.path)
		if got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestPathAccepted(t *testing.T) {
	cases := []struct {
		path    string
		include []string
		exclude []string
		want    bool
	}{
		// No filters -> everything accepted
		{"docs/intro.md", nil, nil, true},

		// Glob include
		{"a/b/match-dir/c.yaml", []string{"**/match-dir/**"}, nil, true},
		{"a/b/other-dir/c.yaml", []string{"**/match-dir/**"}, nil, false},
		{"x/y/z/a.yaml", []string{"x/y/z/**"}, nil, true},
		{"x/y/w/a.yaml", []string{"x/y/z/**"}, nil, false},

		// Multiple includes (OR)
		{"docs/x.md", []string{"docs/**", "design/**"}, nil, true},
		{"design/y.md", []string{"docs/**", "design/**"}, nil, true},
		{"src/main.go", []string{"docs/**", "design/**"}, nil, false},

		// Exclude overrides include
		{"docs/vendor/x.md", []string{"docs/**"}, []string{"docs/vendor/**"}, false},
		{"docs/intro.md", []string{"docs/**"}, []string{"docs/vendor/**"}, true},

		// Exclude alone
		{"vendor/x.go", nil, []string{"vendor/**"}, false},
		{"src/main.go", nil, []string{"vendor/**"}, true},
	}
	for _, c := range cases {
		got := pathAccepted(c.path, c.include, c.exclude)
		if got != c.want {
			t.Errorf("pathAccepted(%q, include=%v, exclude=%v) = %v, want %v",
				c.path, c.include, c.exclude, got, c.want)
		}
	}
}

// TestExpandHome_CacheDir guards against the regression where a leading
// "~/" in cache_dir was treated literally, causing git to clone into a
// directory named "~" relative to the daemon's working directory.
func TestExpandHome_CacheDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	got := expandHome("~/.lth/repos-cache")
	want := filepath.Join(home, ".lth", "repos-cache")
	if got != want {
		t.Errorf("expandHome(~/...) = %q, want %q", got, want)
	}
	// Absolute path should pass through unchanged.
	abs := filepath.Join(string(filepath.Separator), "tmp", "cache")
	if expandHome(abs) != abs {
		t.Errorf("expandHome of absolute path mutated: %q", expandHome(abs))
	}
	// Relative path without leading ~ stays relative.
	rel := filepath.Join("some", "sub")
	if expandHome(rel) != rel {
		t.Errorf("expandHome of relative path mutated: %q", expandHome(rel))
	}
	// Sanity: tilde-expanded result is absolute.
	if !strings.HasPrefix(got, string(filepath.Separator)) {
		t.Errorf("expanded path is not absolute: %q", got)
	}
}

func TestExtensionMatches(t *testing.T) {
	cases := []struct {
		path    string
		allowed []string
		want    bool
	}{
		// Default (empty) -> .md only
		{"a.md", nil, true},
		{"a.MD", nil, true},
		{"a.yaml", nil, false},

		// Explicit list
		{"a.md", []string{".md"}, true},
		{"a.yaml", []string{".md", ".yaml"}, true},
		{"a.YAML", []string{".md", ".yaml"}, true},
		{"a.jsonnet", []string{".md", ".yaml", ".jsonnet"}, true},
		{"a.libsonnet", []string{".md", ".yaml", ".jsonnet"}, false},
		{"a.go", []string{".md", ".yaml"}, false},

		// Bare files (no extension) never match
		{"Makefile", []string{".md"}, false},
		{"Makefile", []string{""}, true}, // odd but consistent
	}
	for _, c := range cases {
		got := extensionMatches(c.path, c.allowed)
		if got != c.want {
			t.Errorf("extensionMatches(%q, %v) = %v, want %v",
				c.path, c.allowed, got, c.want)
		}
	}
}
