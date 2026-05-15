// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"

	"github.com/mattdurham/lth/pkg/lth"
)

// newClientFromGlobalCfg creates a lth.Client from the global config.
func newClientFromGlobalCfg() (*lth.Client, error) {
	if globalCfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return client, nil
}
