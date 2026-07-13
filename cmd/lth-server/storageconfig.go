// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

type StorageConfig struct {
	Provider        string `yaml:"provider"`
	LocalDir        string `yaml:"local_dir"`
	Bucket          string `yaml:"bucket"`
	Endpoint        string `yaml:"endpoint"`
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	UseSSL          bool   `yaml:"use_ssl"`
}
