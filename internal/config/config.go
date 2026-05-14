// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package config provides configuration loading and defaults for lth.
package config

// Config holds all lth configuration loaded from ~/.lth/config.toml.
type Config struct {
	DB struct {
		Path string `toml:"path"`
	} `toml:"db"`

	Embedding struct {
		BaseURL  string `toml:"base_url"`
		Model    string `toml:"model"`
		TimeoutS int    `toml:"timeout_s"`
	} `toml:"embedding"`

	LLM struct {
		BaseURL  string `toml:"base_url"`
		Model    string `toml:"model"`
		TimeoutS int    `toml:"timeout_s"`
	} `toml:"llm"`

	Compaction struct {
		IntervalS       int     `toml:"interval_s"`
		L5Threshold     int     `toml:"l5_threshold"`
		L5MaxAgeH       int     `toml:"l5_max_age_h"`
		L4ClusterSize   int     `toml:"l4_cluster_size"`
		L3EpisodesMin   int     `toml:"l3_episodes_min"`
		L3ImportanceMin float32 `toml:"l3_importance_min"`
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
