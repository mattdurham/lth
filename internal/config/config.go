// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package config provides configuration loading and defaults for lth.
package config

// EmbeddingModel and EmbeddingDim are hard-coded to nomic-embed-text-v1.5.
// Config fields for model/dim are ignored; change these constants to switch models.
const (
	EmbeddingModel  = "nomic-ai/nomic-embed-text-v1.5"
	EmbeddingDim    = 768
	EmbeddingImage  = "ghcr.io/huggingface/text-embeddings-inference:cpu-1.6"
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
}

// LLMChainConfig tunes the fallback chain. All fields optional.
type LLMChainConfig struct {
	CircuitWindow      int     `yaml:"circuit_window"`       // rolling failure window (default 10)
	CircuitFailurePct  float64 `yaml:"circuit_failure_pct"`  // open threshold 0-1 (default 0.5)
	CircuitCooldownS   int     `yaml:"circuit_cooldown_s"`   // skip-duration on open (default 30)
}

// LLMConfig is the llm: section. The top-level fields define the primary
// backend; Fallbacks adds an ordered list tried on transient errors.
type LLMConfig struct {
	LLMBackend `yaml:",inline"`
	Fallbacks  []LLMBackend   `yaml:"fallbacks"`
	Chain      LLMChainConfig `yaml:"chain"`
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
		Paths     []string `yaml:"paths"`
		StateFile string   `yaml:"state_file"`
	} `yaml:"watcher"`
	Issues struct {
		Repos     []string `yaml:"repos"`      // e.g. ["grafana/tempo", "owner/repo"]
		IntervalS int      `yaml:"interval_s"` // default 3600
	} `yaml:"issues"`

	Markdown struct {
		Dirs             []string `yaml:"dirs"`
		Layer            int      `yaml:"layer"`              // default 3
		IntervalS        int      `yaml:"interval_s"`         // default 300
		GitPull          bool     `yaml:"git_pull"`           // run git pull before rescanning; default true
		GitPullIntervalS int      `yaml:"git_pull_interval_s"` // default 3600
	} `yaml:"markdown"`

	Sync struct {
		ServerURL     string `yaml:"server_url"`
		Account       string `yaml:"account"`
		Org           string `yaml:"org"`
		Team          string `yaml:"team"`
		User          string `yaml:"user"`
		AutoIntervalS int    `yaml:"auto_interval_s"`
	} `yaml:"sync"`
}
