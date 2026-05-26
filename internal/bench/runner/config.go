package runner

import "time"

type Config struct {
	ClaudeTimeout time.Duration
	Model         string
	CacheDir      string
}
