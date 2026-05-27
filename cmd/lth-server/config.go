// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds all lth-server configuration loaded from lth-server.yaml.

func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Port: 8080,
		Storage: StorageConfig{
			Provider: "local",
			LocalDir: "~/.lth-server/blobs",
			Region:   "us-east-1",
		},
		Parquet: ParquetConfig{
			Enabled:      true,
			RowGroupSize: 10000,
		},
	}
}

func loadServerConfig(path string) (ServerConfig, error) {
	cfg := defaultServerConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
