package main

type ParquetConfig struct {
	Enabled      bool `yaml:"enabled"`
	RowGroupSize int  `yaml:"row_group_size"`
}
