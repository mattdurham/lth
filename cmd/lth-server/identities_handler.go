// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mattdurham/lth/internal/blobstore"
)

// IdentitiesHandler handles GET /v1/identities.
type IdentitiesHandler struct {
	store blobstore.BlobStore
}

type identitiesResponse struct {
	Accounts []accountEntry `json:"accounts"`
}

type accountEntry struct {
	Account string     `json:"account"`
	Orgs    []orgEntry `json:"orgs"`
}

type orgEntry struct {
	Org   string   `json:"org"`
	Users []string `json:"users"`
}

func (h *IdentitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	keys, err := h.store.List(ctx, "")
	if err != nil {
		http.Error(w, "list store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build account -> org -> users tree from keys like:
	// {account}/{org}/users/{user}/...
	type orgKey struct{ account, org string }
	users := map[orgKey]map[string]struct{}{}

	for _, obj := range keys {
		parts := strings.SplitN(obj.Key, "/", 5)
		if len(parts) < 4 || parts[2] != "users" {
			continue
		}
		account, org, user := parts[0], parts[1], parts[3]
		ok := orgKey{account, org}
		if users[ok] == nil {
			users[ok] = map[string]struct{}{}
		}
		users[ok][user] = struct{}{}
	}

	// Collapse into response structure.
	accounts := map[string]map[string][]string{}
	for ok, userSet := range users {
		if accounts[ok.account] == nil {
			accounts[ok.account] = map[string][]string{}
		}
		ul := make([]string, 0, len(userSet))
		for u := range userSet {
			ul = append(ul, u)
		}
		accounts[ok.account][ok.org] = ul
	}

	resp := identitiesResponse{}
	for account, orgs := range accounts {
		ae := accountEntry{Account: account}
		for org, ul := range orgs {
			ae.Orgs = append(ae.Orgs, orgEntry{Org: org, Users: ul})
		}
		resp.Accounts = append(resp.Accounts, ae)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
