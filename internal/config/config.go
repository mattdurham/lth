// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package config provides configuration loading and defaults for lth.
package config

// EmbeddingModel and EmbeddingDim are hard-coded to nomic-embed-text-v1.5.
// Config fields for model/dim are ignored; change these constants to switch models.
const (
	EmbeddingModel = "nomic-ai/nomic-embed-text-v1.5"
	EmbeddingDim   = 768
	EmbeddingImage = "ghcr.io/huggingface/text-embeddings-inference:cpu-1.6"
)

// LLMBackend describes one LLM endpoint -- either the primary or a fallback.
// Fields mirror the primary LLM block so chains compose cleanly.
type LLMBackend struct {
	Provider             string `yaml:"provider"`               // "anthropic", "openai", "ollama"
	AuthMode             string `yaml:"auth_mode"`              // "api_key" (default) or "oauth" (Anthropic only)
	APIKey               string `yaml:"api_key"`                //nolint:gosec // G117: user-supplied key, not hardcoded
	APIKeyEnv            string `yaml:"api_key_env"`            //nolint:gosec // G117: env var name only
	OAuthCredentialsPath string `yaml:"oauth_credentials_path"` // anthropic-oauth.json path override
	BaseURL              string `yaml:"base_url"`
	Model                string `yaml:"model"`
	TimeoutS             int    `yaml:"timeout_s"`
	// MaxConcurrent caps in-flight LLM calls to this backend. 0 (default) is
	// unbounded -- preserves prior behavior. Set to 1 for a serial local
	// model so concurrent backfill workers queue cleanly client-side rather
	// than piling up inside the inference server.
	MaxConcurrent int `yaml:"max_concurrent"`
}

// LLMChainConfig tunes the fallback chain. All fields optional.
type LLMChainConfig struct {
	CircuitWindow     int     `yaml:"circuit_window"`      // rolling failure window (default 10)
	CircuitFailurePct float64 `yaml:"circuit_failure_pct"` // open threshold 0-1 (default 0.5)
	CircuitCooldownS  int     `yaml:"circuit_cooldown_s"`  // skip-duration on open (default 30)
}

// LLMConfig is the llm: section. The top-level fields define the primary
// backend; Fallbacks adds an ordered list tried on transient errors.
type LLMConfig struct {
	LLMBackend `yaml:",inline"`
	Fallbacks  []LLMBackend   `yaml:"fallbacks"`
	Chain      LLMChainConfig `yaml:"chain"`
}

// MarkdownGitHubRepo is one GitHub repo to clone and scan.
//
// Repo is "<org>/<name>". Include and Exclude are lists of slash-style globs
// matched against the file path relative to the repo root. Supported syntax:
// `*` (single path segment), `?` (single char in a segment), and `**` (zero
// or more path segments). Examples:
//
//	**/foo/**             any path with a "foo" directory component
//	component/sub/**      only that subtree
//	docs/**/*.md          any .md under docs/, at any depth
//
// FileTypes is the list of file extensions to ingest (e.g. ".md", ".yaml",
// ".jsonnet"). Matched case-insensitively. Empty defaults to [".md"] for
// back-compat with the original markdown-only watcher. Branch is the branch
// to track; empty means the repo's default branch.
//
// Example:
//
//	repo: acme/widgets
//	include: ["**/tempo/**"]
//	exclude: ["**/vendor/**"]
//	file_types: [".md", ".yaml", ".jsonnet"]
type MarkdownGitHubRepo struct {
	Repo      string   `yaml:"repo"`
	Include   []string `yaml:"include"`
	Exclude   []string `yaml:"exclude"`
	FileTypes []string `yaml:"file_types"`
	Branch    string   `yaml:"branch"`
}

// MarkdownGitHub configures the optional GitHub-repo source for the markdown
// watcher. CacheDir defaults to ~/.lth/repos-cache; CloneDepth defaults to 1
// (shallow). Auth is delegated to local git (SSH keys, credential helpers).
type MarkdownGitHub struct {
	CacheDir   string               `yaml:"cache_dir"`
	CloneDepth int                  `yaml:"clone_depth"`
	Repos      []MarkdownGitHubRepo `yaml:"repos"`
}

// GWSConfig configures the Google Workspace watcher. Disabled by default.
// Requires the `gws` CLI (Google Workspace CLI from npm) to be installed and
// authenticated; lth never handles credentials directly.
//
// On each tick, the watcher queries Drive for documents matching any of the
// configured name patterns modified within the lookback window, downloads
// their content via the Docs API, and writes one markdown file per document
// into OutputDir. The markdown watcher then picks them up on its normal scan
// cycle.
type GWSConfig struct {
	Enabled         bool     `yaml:"enabled"`          // master switch; default false
	IntervalH       int      `yaml:"interval_h"`       // poll cadence in hours; default 3
	LookbackDays    int      `yaml:"lookback_days"`    // only fetch docs modified within this window; default 14
	OutputDir       string   `yaml:"output_dir"`       // where to write the .md files; default ~/.lth/gws-imports
	NamePatterns    []string `yaml:"name_patterns"`    // Drive name `contains` patterns (OR'd together); default ["Notes by Gemini", "Transcript"]
	ExcludePatterns []string `yaml:"exclude_patterns"` // optional name patterns to skip
	GWSBinary       string   `yaml:"gws_binary"`       // override path to the gws executable; default looks up $PATH
}

// PRSource is one repo to mine for merged PR history. Repo is the GitHub
// "<org>/<name>" slug, used for `gh` lookups, as the clone URL, and as the
// project attribute on stored memories. Path, if set, points at an existing
// local git checkout to use as-is (only fast-forward-pulled, never cloned or
// reset). If empty, lth clones/updates the repo itself into
// Markdown.GitHub.CacheDir (the same `~/.lth/repos-cache/<org>/<name>/`
// directory the markdown watcher's GitHub-repos feature uses), always as a
// full (non-shallow) clone regardless of any depth the markdown watcher may
// have used for it, so history mining always sees the whole repo. Dir, if
// set, scopes the git log walk to a subdirectory (e.g.
// "ksonnet/environments/tempo"); empty means the whole repo.
type PRSource struct {
	Repo string `yaml:"repo"`
	Path string `yaml:"path"`
	Dir  string `yaml:"dir"`
}

// PRConfig configures the PR-history watcher: for each configured source, it
// finds commits under Dir via `git log`, resolves the merged PR behind each
// new commit via `gh`, and stores an LLM-written summary of each new PR as a
// memory backdated to the PR's merge time (see memory.Store's "created_at"
// attr) so old PRs decay in search like old memories instead of scoring as
// freshly created.
type PRConfig struct {
	Sources []PRSource `yaml:"sources"`
	// IntervalS is the poll cadence; default 21600 (6h) -- PR history changes slowly.
	IntervalS int `yaml:"interval_s"`
	// Layer is the memory layer summaries are stored at; default 5.
	Layer int `yaml:"layer"`
	// LookbackDays bounds how far back into history a source is mined. 0
	// (the default) means unbounded -- mine the source's entire history.
	// Regardless of this setting, MaxPerScan bounds how much work is done in
	// any single scan, so even an unbounded source replays its full history
	// gradually across many scans rather than bursting all at once.
	LookbackDays int `yaml:"lookback_days"`
	// MaxPerScan caps how many new PRs are resolved and attempted in a single
	// scan, shared across all sources, spreading a large backlog (or an
	// unbounded LookbackDays) over multiple ticks instead of bursting dozens
	// of gh/LLM calls at once. Default 10.
	MaxPerScan int `yaml:"max_per_scan"`
	// SkipAuthors excludes commits/PRs authored by these GitHub logins (bots,
	// automation) from being summarized. Default: common bot logins.
	SkipAuthors []string `yaml:"skip_authors"`
}

// BackupConfig configures the daily database snapshot watcher. It takes a
// consistent VACUUM INTO copy of the database, gzips it into Dir, and keeps
// only the Keep most recent snapshots. Disabled until Dir is set -- there is
// deliberately no default directory, since a default under lth's own data
// dir would likely put backups on the same disk as the database they exist
// to protect against.
type BackupConfig struct {
	// Dir is where snapshots are written. Required; empty disables the watcher.
	Dir string `yaml:"dir"`
	// IntervalH is the poll cadence in hours; default 24. A simple ticker, not
	// a wall-clock schedule -- there is no "run at 3am" semantics.
	IntervalH int `yaml:"interval_h"`
	// Keep is how many of the most recent snapshots to retain; default 7.
	// Count-based, not age-based: always keeps exactly this many files
	// regardless of gaps (e.g. the daemon being down for a few days).
	Keep int `yaml:"keep"`
}

// Config holds all lth configuration loaded from ~/.lth/config.yaml.
type Config struct {
	DB struct {
		Path string `yaml:"path"`
	} `yaml:"db"`

	Embedding struct {
		Provider   string `yaml:"provider"`    // "huggingface" (default) or "ollama"
		AutoDocker bool   `yaml:"auto_docker"` // auto-start TEI via Docker (default: true)
		DockerPort int    `yaml:"docker_port"` // default: 8080
		BaseURL    string `yaml:"base_url"`
		TimeoutS   int    `yaml:"timeout_s"`
	} `yaml:"embedding"`

	LLM LLMConfig `yaml:"llm"`

	Compaction struct {
		IntervalS            int     `yaml:"interval_s"`
		L5Threshold          int     `yaml:"l_5_threshold"`
		L5MaxAgeH            int     `yaml:"l_5_max_age_h"`
		L5ClusterThreshold   float32 `yaml:"l_5_cluster_threshold"`
		L5MinClusterSize     int     `yaml:"l_5_min_cluster_size"`
		L5MaxClusterChars    int     `yaml:"l_5_max_cluster_chars"` // sample down to this prompt-content budget before summarizing (default: 80000 ~= 20k tokens)
		L4ClusterSize        int     `yaml:"l_4_cluster_size"`
		L3EpisodesMin        int     `yaml:"l_3_episodes_min"`
		L3ImportanceMin      float32 `yaml:"l_3_importance_min"`
		SeedMinL2            int     `yaml:"seed_min_l_2"`           // auto-seed L2 when count < this -- default: 10
		SeedMinL3            int     `yaml:"seed_min_l_3"`           // auto-seed L3 when count < this -- default: 20
		SeedSample           int     `yaml:"seed_sample"`            // L5 memories to sample per seed run -- default: 100
		ValenceCompactionMin float32 `yaml:"valence_compaction_min"` // L4 memories with |valence| < this are noise -- default: 0.15
	} `yaml:"compaction"`

	Search struct {
		DefaultTopK int     `yaml:"default_top_k"`
		Alpha       float32 `yaml:"alpha"`
		Beta        float32 `yaml:"beta"`
		Gamma       float32 `yaml:"gamma"`
	} `yaml:"search"`

	Watcher struct {
		Paths         []string `yaml:"paths"`
		StateFile     string   `yaml:"state_file"`
		LogRetainDays int      `yaml:"log_retain_days"` // daemon.log rotation retention (default 3)
	} `yaml:"watcher"`
	Issues struct {
		Repos     []string `yaml:"repos"`      // e.g. ["grafana/tempo", "owner/repo"]
		IntervalS int      `yaml:"interval_s"` // default 3600
	} `yaml:"issues"`

	Markdown struct {
		Dirs             []string       `yaml:"dirs"`
		Layer            int            `yaml:"layer"`               // default 3
		IntervalS        int            `yaml:"interval_s"`          // default 300
		GitPull          bool           `yaml:"git_pull"`            // run git pull before rescanning; default true
		GitPullIntervalS int            `yaml:"git_pull_interval_s"` // default 3600
		GitHub           MarkdownGitHub `yaml:"github"`              // optional auto-cloned GitHub repos
	} `yaml:"markdown"`

	GWS GWSConfig `yaml:"gws"`

	PR PRConfig `yaml:"pr"`

	Backup BackupConfig `yaml:"backup"`

	Sync struct {
		ServerURL     string `yaml:"server_url"`
		Account       string `yaml:"account"`
		Org           string `yaml:"org"`
		Team          string `yaml:"team"`
		User          string `yaml:"user"`
		AutoIntervalS int    `yaml:"auto_interval_s"`
	} `yaml:"sync"`

	// API controls the REST API exposed by the daemon on the metrics port and
	// the optional proxy mode where CLI commands forward requests to a remote
	// daemon instead of opening a local DB connection.
	API struct {
		// Enabled controls whether the daemon registers the /api/v1/ route set
		// on startup. Default: false. Requires daemon restart to change.
		Enabled bool `yaml:"enabled"`
		// ListenAddr is the host:port the metrics+API HTTP server binds to.
		// Default: "localhost:10010". Set to "0.0.0.0:10010" to accept
		// connections from other machines. Requires daemon restart to change.
		ListenAddr string `yaml:"listen_addr"`
		// ProxyURL, when non-empty, causes every CLI command to proxy its
		// request to the lth daemon at this URL instead of opening a local DB
		// connection. Example: "http://localhost:10010". No local daemon is
		// started when this is set.
		ProxyURL string `yaml:"proxy_url"`
	} `yaml:"api"`
}
