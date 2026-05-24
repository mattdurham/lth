// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/spf13/cobra"
)

var (
	syncPushChunkSize int
	syncPullSince     string
	syncPullLayers    string
	syncFlagAccount   string
	syncFlagOrg       string
	syncFlagUser      string
	syncFlagTeam      string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync memories with lth-server",
}

var syncPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push local memories to lth-server (excludes source=server memories)",
	RunE:  runSyncPush,
}

var syncPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull memories from lth-server",
	RunE:  runSyncPull,
}

var syncBothCmd = &cobra.Command{
	Use:   "sync",
	Short: "Push then pull (bidirectional sync)",
	RunE:  runSyncBoth,
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync configuration and pending count",
	RunE:  runSyncStatus,
}

func init() {
	syncPushCmd.Flags().IntVar(&syncPushChunkSize, "chunk-size", 1000, "records per JSONL chunk")
	syncPushCmd.Flags().StringVar(&syncFlagAccount, "account", "", "override config sync.account")
	syncPushCmd.Flags().StringVar(&syncFlagOrg, "org", "", "override config sync.org")
	syncPushCmd.Flags().StringVar(&syncFlagUser, "user", "", "override config sync.user")
	syncPushCmd.Flags().StringVar(&syncFlagTeam, "team", "", "override config sync.team")

	syncPullCmd.Flags().StringVar(&syncPullSince, "since", "", "pull memories at or after this RFC3339 timestamp (default: all)")
	syncPullCmd.Flags().StringVar(&syncPullLayers, "layers", "1,2,3,4", "comma-separated layer numbers to pull (1-4)")
	syncPullCmd.Flags().StringVar(&syncFlagAccount, "account", "", "override config sync.account")
	syncPullCmd.Flags().StringVar(&syncFlagOrg, "org", "", "override config sync.org")
	syncPullCmd.Flags().StringVar(&syncFlagUser, "user", "", "override config sync.user")
	syncPullCmd.Flags().StringVar(&syncFlagTeam, "team", "", "override config sync.team")

	syncCmd.AddCommand(syncPushCmd, syncPullCmd, syncBothCmd, syncStatusCmd)
	rootCmd.AddCommand(syncCmd)
}

type syncCfg struct {
	serverURL string
	account   string
	org       string
	user      string
	team      string
}

func effectiveSyncCfg() (syncCfg, error) {
	if globalCfg == nil {
		return syncCfg{}, fmt.Errorf("config not loaded")
	}
	c := syncCfg{
		serverURL: globalCfg.Sync.ServerURL,
		account:   globalCfg.Sync.Account,
		org:       globalCfg.Sync.Org,
		user:      globalCfg.Sync.User,
		team:      globalCfg.Sync.Team,
	}
	if syncFlagAccount != "" {
		c.account = syncFlagAccount
	}
	if syncFlagOrg != "" {
		c.org = syncFlagOrg
	}
	if syncFlagUser != "" {
		c.user = syncFlagUser
	}
	if syncFlagTeam != "" {
		c.team = syncFlagTeam
	}
	return c, nil
}

func (s syncCfg) validate() error {
	if s.serverURL == "" {
		return fmt.Errorf("sync.server_url is not configured; set [sync] server_url in ~/.lth/config.toml")
	}
	if s.account == "" {
		return fmt.Errorf("sync.account is not configured")
	}
	if s.org == "" {
		return fmt.Errorf("sync.org is not configured")
	}
	if s.user == "" {
		return fmt.Errorf("sync.user is not configured")
	}
	return nil
}

func (s syncCfg) headers() map[string]string {
	h := map[string]string{
		"X-LTH-Account": s.account,
		"X-LTH-Org":     s.org,
		"X-LTH-User":    s.user,
	}
	if s.team != "" {
		h["X-LTH-Team"] = s.team
	}
	return h
}

func runSyncPush(cmd *cobra.Command, _ []string) error {
	cfg, err := effectiveSyncCfg()
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	d, err := db.Open(globalCfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	ctx := cmd.Context()
	payload, count, err := exportDBFiltered(ctx, d, "server", syncPushChunkSize)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	resp, err := doSyncRequest(ctx, http.MethodPost, cfg.serverURL+"/v1/sync/push", cfg.headers(),
		bytes.NewReader(payload), "application/zip")
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Accepted int `json:"accepted"`
		Skipped  int `json:"skipped"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("Pushed %d memories (accepted=%d skipped=%d)\n", count, result.Accepted, result.Skipped)
	return nil
}

func runSyncPull(cmd *cobra.Command, _ []string) error {
	cfg, err := effectiveSyncCfg()
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	layers, err := parseSyncLayers(syncPullLayers)
	if err != nil {
		return err
	}

	url := cfg.serverURL + "/v1/sync/pull?layers=" + syncPullLayers
	if syncPullSince != "" {
		if _, err := time.Parse(time.RFC3339, syncPullSince); err != nil {
			return fmt.Errorf("invalid --since value %q: must be RFC3339: %w", syncPullSince, err)
		}
		url += "&since=" + syncPullSince
	}
	_ = layers

	resp, err := doSyncRequest(cmd.Context(), http.MethodGet, url, cfg.headers(), nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	d, err := db.Open(globalCfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	imported, skipped, err := importFromZIPReader(cmd.Context(), d, resp.Body)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	fmt.Printf("Pulled %d memories (%d skipped)\n", imported, skipped)
	return nil
}

func runSyncBoth(cmd *cobra.Command, _ []string) error {
	if err := runSyncPush(cmd, nil); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return runSyncPull(cmd, nil)
}

func runSyncStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := effectiveSyncCfg()
	if err != nil {
		return err
	}

	fmt.Printf("Server URL:  %s\n", cfg.serverURL)
	fmt.Printf("Account:     %s\n", cfg.account)
	fmt.Printf("Org:         %s\n", cfg.org)
	fmt.Printf("User:        %s\n", cfg.user)
	if cfg.team != "" {
		fmt.Printf("Team:        %s\n", cfg.team)
	}

	if cfg.serverURL == "" {
		fmt.Fprintln(os.Stderr, "warning: server_url not configured")
		return nil
	}

	d, err := db.Open(globalCfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	stats, err := d.Stats(cmd.Context())
	if err != nil {
		return fmt.Errorf("db stats: %w", err)
	}

	pending := 0
	for _, count := range stats.ByLayer {
		pending += count
	}
	fmt.Printf("Local memories (pending push): ~%d\n", pending)
	return nil
}

// parseLayers parses a comma-separated layer string; returns error if any layer is outside 1-4.
func parseSyncLayers(s string) ([]int, error) {
	if s == "" {
		return []int{1, 2, 3, 4}, nil
	}
	parts := strings.Split(s, ",")
	layers := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid layer %q: %w", p, err)
		}
		if n < 1 || n > 4 {
			return nil, fmt.Errorf("layer %d out of range: must be 1-4 (L5 has no pull endpoint)", n)
		}
		layers = append(layers, n)
	}
	if len(layers) == 0 {
		return []int{1, 2, 3, 4}, nil
	}
	return layers, nil
}

// exportDBFiltered exports active memories with source != excludeSource to a ZIP buffer.
func exportDBFiltered(ctx context.Context, d *db.DB, excludeSource string, chunkSize int) ([]byte, int, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	layerOrder := []int{5, 4, 3, 2, 1}
	layerRows := make(map[int][]*db.MemoryRow, len(layerOrder))
	totalCount := 0

	for _, layer := range layerOrder {
		rows, err := d.ListLayer(ctx, layer, true)
		if err != nil {
			zw.Close() //nolint:errcheck
			return nil, 0, fmt.Errorf("list layer %d: %w", layer, err)
		}
		filtered := rows[:0]
		for _, r := range rows {
			if r.Source != excludeSource {
				filtered = append(filtered, r)
			}
		}
		layerRows[layer] = filtered
		totalCount += len(filtered)
	}

	edges, err := d.GetAllEdges(ctx)
	if err != nil {
		zw.Close() //nolint:errcheck
		return nil, 0, fmt.Errorf("get edges: %w", err)
	}

	now := time.Now().UTC()
	layerCounts := make(map[string]int)
	for layer, rows := range layerRows {
		if len(rows) > 0 {
			layerCounts[strconv.Itoa(layer)] = len(rows)
		}
	}

	metadata := exportMetadata{
		LTHVersion:  lthVersion,
		ExportedAt:  now,
		MemoryCount: totalCount,
		EdgeCount:   len(edges),
		ChunkSize:   chunkSize,
		LayerCounts: layerCounts,
	}
	if err := writeZIPJSON(zw, "metadata.json", metadata); err != nil {
		zw.Close() //nolint:errcheck
		return nil, 0, fmt.Errorf("write metadata: %w", err)
	}

	var files []string
	totalMemories := 0

	for _, layer := range layerOrder {
		rows := layerRows[layer]
		if len(rows) == 0 {
			continue
		}
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		attrs, err := d.GetAttributesBatch(ctx, ids)
		if err != nil {
			zw.Close() //nolint:errcheck
			return nil, 0, fmt.Errorf("get attributes layer %d: %w", layer, err)
		}
		chunkNum := 0
		for start := 0; start < len(rows); start += chunkSize {
			end := start + chunkSize
			if end > len(rows) {
				end = len(rows)
			}
			chunk := rows[start:end]
			records := make([]exportMemory, len(chunk))
			for i, r := range chunk {
				em := exportMemory{
					ID:             r.ID,
					Layer:          r.Layer,
					Content:        r.Content,
					ContentHash:    r.ContentHash,
					Embedding:      vector.FromBytes(r.Embedding),
					Importance:     r.Importance,
					AccessCount:    r.AccessCount,
					CreatedAt:      r.CreatedAt.UTC(),
					UpdatedAt:      r.UpdatedAt.UTC(),
					LastAccessedAt: r.LastAccessedAt.UTC(),
					DecayRate:      r.DecayRate,
					Stability:      r.Stability,
					Source:         r.Source,
					Agent:          r.Agent,
					Valence:        r.Valence,
					ValenceScored:  r.ValenceScored,
				}
				if a := attrs[r.ID]; len(a) > 0 {
					em.Attrs = a
				}
				records[i] = em
			}
			filename := fmt.Sprintf("memories_l%d_%03d.jsonl", layer, chunkNum)
			if err := writeMemoryChunk(zw, filename, records); err != nil {
				zw.Close() //nolint:errcheck
				return nil, 0, fmt.Errorf("write chunk %s: %w", filename, err)
			}
			files = append(files, filename)
			totalMemories += len(chunk)
			chunkNum++
		}
	}

	if len(edges) > 0 {
		chunkNum := 0
		for start := 0; start < len(edges); start += chunkSize {
			end := start + chunkSize
			if end > len(edges) {
				end = len(edges)
			}
			chunk := edges[start:end]
			records := make([]exportEdge, len(chunk))
			for i, e := range chunk {
				records[i] = exportEdge{
					ID:        e.ID,
					FromID:    e.FromID,
					ToID:      e.ToID,
					EdgeType:  e.EdgeType,
					Weight:    e.Weight,
					CreatedAt: e.CreatedAt.UTC(),
				}
			}
			filename := fmt.Sprintf("edges_%03d.jsonl", chunkNum)
			if err := writeEdgeChunk(zw, filename, records); err != nil {
				zw.Close() //nolint:errcheck
				return nil, 0, fmt.Errorf("write edge chunk %s: %w", filename, err)
			}
			files = append(files, filename)
			chunkNum++
		}
	}

	manifest := exportManifest{
		ExportedAt:  now,
		ChunkSize:   chunkSize,
		MemoryCount: totalMemories,
		EdgeCount:   len(edges),
		Files:       files,
	}
	if err := writeZIPJSON(zw, "manifest.json", manifest); err != nil {
		zw.Close() //nolint:errcheck
		return nil, 0, fmt.Errorf("write manifest: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, 0, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), totalMemories, nil
}

// doSyncRequest performs an HTTP request with identity headers.
func doSyncRequest(ctx context.Context, method, url string, headers map[string]string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, url, err)
	}
	return resp, nil
}

// importFromZIPReader reads a ZIP archive from r and imports memories with source="server".
func importFromZIPReader(ctx context.Context, d *db.DB, r io.Reader) (imported, skipped int, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, 0, fmt.Errorf("read body: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, fmt.Errorf("open zip: %w", err)
	}
	manifest, err := readManifest(zr)
	if err != nil {
		return 0, 0, err
	}
	for _, filename := range manifest.Files {
		f := findZipFile(zr, filename)
		if f == nil {
			return 0, 0, fmt.Errorf("file %q listed in manifest not found in archive", filename)
		}
		rc, err := f.Open()
		if err != nil {
			return 0, 0, fmt.Errorf("open zip entry %q: %w", filename, err)
		}
		if strings.HasPrefix(filename, "memories_") {
			n, skip, iErr := importMemoriesServerSource(ctx, d, rc)
			rc.Close() //nolint:errcheck
			if iErr != nil {
				return imported, skipped, fmt.Errorf("import %s: %w", filename, iErr)
			}
			imported += n
			skipped += skip
		} else {
			rc.Close() //nolint:errcheck
		}
	}
	return imported, skipped, nil
}

// importMemoriesServerSource imports memories, forcing source="server" on all records.
func importMemoriesServerSource(ctx context.Context, d *db.DB, rc interface{ Read([]byte) (int, error) }) (imported, skipped int, err error) {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var em exportMemory
		if err := json.Unmarshal(line, &em); err != nil {
			return imported, skipped, fmt.Errorf("decode memory: %w", err)
		}
		row := &db.MemoryRow{
			ID:             em.ID,
			Layer:          em.Layer,
			Content:        em.Content,
			ContentHash:    em.ContentHash,
			Embedding:      vector.ToBytes(em.Embedding),
			Importance:     em.Importance,
			AccessCount:    em.AccessCount,
			CreatedAt:      em.CreatedAt,
			UpdatedAt:      em.UpdatedAt,
			LastAccessedAt: em.LastAccessedAt,
			DecayRate:      em.DecayRate,
			Stability:      em.Stability,
			Source:         "server",
			Agent:          em.Agent,
			Valence:        em.Valence,
			ValenceScored:  em.ValenceScored,
		}
		insertErr := d.InsertMemory(ctx, row)
		if insertErr != nil {
			if strings.Contains(insertErr.Error(), "UNIQUE constraint failed") {
				skipped++
				continue
			}
			return imported, skipped, fmt.Errorf("insert memory %q: %w", em.ID, insertErr)
		}
		if len(em.Attrs) > 0 {
			if err := d.SetAttributes(ctx, em.ID, em.Attrs); err != nil {
				return imported, skipped, fmt.Errorf("set attributes %q: %w", em.ID, err)
			}
		}
		imported++
	}
	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("scan lines: %w", err)
	}
	return imported, skipped, nil
}
