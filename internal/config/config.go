// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package config provides configuration loading and defaults for lth.
package config

// Config holds all lth configuration loaded from ~/.lth/config.toml.
type Config struct {
	DB struct {
		Path string `toml:"path"`
	} `toml:"db"`

	Embedding struct {
		Provider        string `toml:"provider"`           // "huggingface", "ollama", "openai" — default: "huggingface"
		AutoDocker      bool   `toml:"auto_docker"`        // default: true for huggingface provider
		DockerImage     string `toml:"docker_image"`       // default: "ghcr.io/huggingface/text-embeddings-inference:cpu-latest"
		DockerPort      int    `toml:"docker_port"`        // default: 8080
		BaseURL         string `toml:"base_url"`
		Model           string `toml:"model"`
		TimeoutS        int    `toml:"timeout_s"`
		TrustRemoteCode bool   `toml:"trust_remote_code"` // required for models with custom pooling (e.g. nomic-embed-text)
	} `toml:"embedding"`

	LLM struct {
		Provider string `toml:"provider"` // "anthropic", "ollama", "openai" — default: "anthropic"
		APIKey   string `toml:"api_key"`  //nolint:gosec // G117: not a hardcoded secret — config field for user-supplied key
		BaseURL  string `toml:"base_url"`
		Model    string `toml:"model"`
		TimeoutS int    `toml:"timeout_s"`
	} `toml:"llm"`

	Compaction struct {
		IntervalS            int     `toml:"interval_s"`
		L5Threshold          int     `toml:"l5_threshold"`
		L5MaxAgeH            int     `toml:"l5_max_age_h"`
		L5ClusterThreshold   float32 `toml:"l5_cluster_threshold"`
		L5MinClusterSize     int     `toml:"l5_min_cluster_size"`
		L4ClusterSize        int     `toml:"l4_cluster_size"`
		L3EpisodesMin        int     `toml:"l3_episodes_min"`
		L3ImportanceMin      float32 `toml:"l3_importance_min"`
		SeedMinL2            int     `toml:"seed_min_l2"`            // auto-seed L2 when count < this — default: 10
		SeedMinL3            int     `toml:"seed_min_l3"`            // auto-seed L3 when count < this — default: 20
		SeedSample           int     `toml:"seed_sample"`            // L5 memories to sample per seed run — default: 100
		ValenceCompactionMin float32 `toml:"valence_compaction_min"` // L4 memories with |valence| < this are noise — default: 0.15
	} `toml:"compaction"`

	Search struct {
		DefaultTopK int     `toml:"default_top_k"`
		Alpha       float32 `toml:"alpha"`
		Beta        float32 `toml:"beta"`
		Gamma       float32 `toml:"gamma"`
	} `toml:"search"`

	Watcher struct {
		Paths     []string `toml:"paths"`
		StateFile string   `toml:"state_file"`
	} `toml:"watcher"`
}
