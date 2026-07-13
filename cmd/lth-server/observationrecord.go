// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import "time"

type observationRecord struct {
	Content   string            `json:"content"`
	Agent     string            `json:"agent,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	Valence   float32           `json:"valence"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
}
