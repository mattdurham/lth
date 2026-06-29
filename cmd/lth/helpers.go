// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"fmt"

	"github.com/mattdurham/lth/internal/proxyclient"
	"github.com/mattdurham/lth/pkg/lth"
)

// MemClient is the interface satisfied by both *lth.Client (local DB) and
// *proxyclient.Client (HTTP proxy). All CLI commands that touch memory use
// this interface so they work transparently in proxy mode.
type MemClient interface {
	Close() error
	Store(ctx context.Context, layer int, content string, attrs map[string]string) (*lth.Memory, error)
	Search(ctx context.Context, req *lth.SearchRequest) ([]*lth.SearchResult, error)
	Get(ctx context.Context, id string) (*lth.Memory, error)
	Stats(ctx context.Context) (*lth.Stats, error)
	ListLayer(ctx context.Context, layer int) ([]*lth.Memory, error)
	SoftDelete(ctx context.Context, ids []string, reason string) error
	MergeAttr(ctx context.Context, id, key, value string) error
	DistinctAttrValues(ctx context.Context, key string) ([]string, error)
	GraphNeighbors(ctx context.Context, id string, depth int) ([]*lth.Edge, error)
	GraphPPR(ctx context.Context, seeds []string) (map[string]float64, error)
}

// newClientFromGlobalCfg creates a MemClient from the global config.
// When api.proxy_url is set in the config the client proxies all requests to
// the remote daemon over HTTP; otherwise it opens a local DB connection.
func newClientFromGlobalCfg() (MemClient, error) {
	if globalCfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	if globalCfg.API.ProxyURL != "" {
		return proxyclient.New(globalCfg.API.ProxyURL), nil
	}
	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return client, nil
}
