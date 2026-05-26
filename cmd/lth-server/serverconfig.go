package main

type ServerConfig struct {
	Port    int           `yaml:"port"`
	Storage StorageConfig `yaml:"storage"`
	Parquet ParquetConfig `yaml:"parquet"`
}
