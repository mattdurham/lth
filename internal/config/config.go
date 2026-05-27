// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package config provides configuration loading and defaults for lth.
package config

// Config holds all lth configuration loaded from ~/.lth/config.yaml.
type Config struct {
	DB struct {
		Path string `yaml:"path"`
	} `yaml:"db"`

	Embedding struct {
		Provider        string `yaml:"provider"`     // "huggingface", "ollama", "openai" -- default: "huggingface"
		AutoDocker      bool   `yaml:"auto_docker"`  // default: true for huggingface provider
		DockerImage     string `yaml:"docker_image"` // default: "ghcr.io/huggingface/text-embeddings-inference:cpu-latest"
		DockerPort      int    `yaml:"docker_port"`  // default: 8080
		BaseURL         string `yaml:"base_url"`
		Model           string `yaml:"model"`
		TimeoutS        int    `yaml:"timeout_s"`
		TrustRemoteCode bool   `yaml:"trust_remote_code"` // required for models with custom pooling (e.g. nomic-embed-text)
		Dim             int    `yaml:"dim"`               // embedding output dimension (e.g. 768 for nomic, 1024 for mxbai)
	} `yaml:"embedding"`

	LLM struct {
		Provider string `yaml:"provider"` // "anthropic", "ollama", "openai" -- default: "anthropic"
		APIKey   string `yaml:"api_key"`  //nolint:gosec // G117: not a hardcoded secret -- config field for user-supplied key
		BaseURL  string `yaml:"base_url"`
		Model    string `yaml:"model"`
		TimeoutS int    `yaml:"timeout_s"`
	} `yaml:"llm"`

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
	Sync struct {
		ServerURL     string `yaml:"server_url"`
		Account       string `yaml:"account"`
		Org           string `yaml:"org"`
		Team          string `yaml:"team"`
		User          string `yaml:"user"`
		AutoIntervalS int    `yaml:"auto_interval_s"`
	} `yaml:"sync"`
}
