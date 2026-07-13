// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

type ServerConfig struct {
	Port     int           `yaml:"port"`
	Storage  StorageConfig `yaml:"storage"`
	Parquet  ParquetConfig `yaml:"parquet"`
	BindAddr string        `yaml:"bind_addr"`
}
