// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

// ParquetConfig holds parquet write configuration for lth-server.
type ParquetConfig struct {
	Enabled      bool `yaml:"enabled"`
	RowGroupSize int  `yaml:"row_group_size"`
}
