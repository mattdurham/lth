// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import "time"

type Config struct {
	ClaudeTimeout time.Duration
	Model         string
	CacheDir      string
}
