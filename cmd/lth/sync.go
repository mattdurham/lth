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
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/metrics"
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
	syncPullCmd.Flags().StringVar(&syncPullLayers, "layers", "1,2,3,4,5", "comma-separated layer numbers to pull (1-5)")
	syncPullCmd.Flags().StringVar(&syncFlagAccount, "account", "", "override config sync.account")
	syncPullCmd.Flags().StringVar(&syncFlagOrg, "org", "", "override config sync.org")
	syncPullCmd.Flags().StringVar(&syncFlagUser, "user", "", "override config sync.user")
	syncPullCmd.Flags().StringVar(&syncFlagTeam, "team", "", "override config sync.team")

	syncCmd.AddCommand(syncPushCmd, syncPullCmd, syncBothCmd, syncStatusCmd)
	rootCmd.AddCommand(syncCmd)
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
		return fmt.Errorf("sync.server_url is not configured; set server_url in ~/.lth/config.yaml")
	}
	if s.account == "" {
		s.account = "default"
	}
	if s.org == "" {
		s.org = "default"
	}
	if s.user == "" {
		s.user = "default"
	}
	if globalCfg.Sync.Account == "default" {
		return fmt.Errorf("sync.account: default is a reserved keyword")
	}
	if globalCfg.Sync.Org == "default" {
		return fmt.Errorf("sync.org: default is a reserved keyword")
	}
	if globalCfg.Sync.User == "default" {
		return fmt.Errorf("sync.user: default is a reserved keyword")
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
	return syncPush(cmd.Context(), nil)
}

func syncPush(ctx context.Context, m *metrics.Metrics) error {
	cfg, err := effectiveSyncCfg()
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	d, err := db.Open(globalCfg.DB.Path, 0)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

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
	if m != nil {
		m.SyncPushedTotal.WithLabelValues("ok").Add(float64(result.Accepted))
	}
	if err := d.MarkPushed(ctx, time.Now().UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("mark pushed failed", "err", err)
	}
	return nil
}

func runSyncPull(cmd *cobra.Command, _ []string) error {
	return syncPull(cmd.Context(), nil)
}

func syncPull(ctx context.Context, m *metrics.Metrics) error {
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

	resp, err := doSyncRequest(ctx, http.MethodGet, url, cfg.headers(), nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Download to a temp file first so the HTTP connection is released before we hold the DB lock.
	tmp, err := os.CreateTemp("", "lth-pull-*.zip")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("download pull: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("seek temp file: %w", err)
	}
	resp.Body.Close() //nolint:errcheck

	d, err := db.Open(globalCfg.DB.Path, 0)
	if err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	start := time.Now()
	imported, skipped, err := importFromZIPReader(ctx, d, tmp)
	tmp.Close() //nolint:errcheck
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	fmt.Printf("Pulled %d memories (%d skipped) in %s\n", imported, skipped, elapsed.Round(time.Millisecond))
	if m != nil {
		m.SyncPulledTotal.WithLabelValues("ok").Add(float64(imported))
		m.SyncDurationSeconds.WithLabelValues("pull").Observe(elapsed.Seconds())
	}
	return nil
}

func runSyncBoth(cmd *cobra.Command, _ []string) error {
	return syncBoth(cmd.Context(), nil)
}

func syncBoth(ctx context.Context, m *metrics.Metrics) error {
	if err := syncPush(ctx, m); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return syncPull(ctx, m)
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

	d, err := db.Open(globalCfg.DB.Path, 0)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	var pending int
	if err := d.CountPendingPush(cmd.Context(), &pending); err != nil {
		return fmt.Errorf("count pending push: %w", err)
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
		if n < 1 || n > 5 {
			return nil, fmt.Errorf("layer %d out of range: must be 1-5", n)
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

const importBatchSize = 500

// importMemoriesServerSource imports memories, forcing source="server" on all records.
func importMemoriesServerSource(ctx context.Context, d *db.DB, rc interface{ Read([]byte) (int, error) }) (imported, skipped int, err error) {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	batch := make([]*db.MemoryRow, 0, importBatchSize)
	batchAttrs := make(map[string]map[string]string)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, u, s, err := d.InsertMemoryBatch(ctx, batch, batchAttrs)
		imported += n + u
		skipped += s
		batch = batch[:0]
		batchAttrs = make(map[string]map[string]string)
		fmt.Printf("  importing... %d written, %d skipped\n", imported, skipped)
		return err
	}

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
			Embedding:      nil, // stripped on import; BackfillEmbeddings re-embeds locally
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
		batch = append(batch, row)
		if len(em.Attrs) > 0 {
			batchAttrs[em.ID] = em.Attrs
		}
		if len(batch) >= importBatchSize {
			if err := flush(); err != nil {
				return imported, skipped, fmt.Errorf("flush batch: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("scan lines: %w", err)
	}
	if err := flush(); err != nil {
		return imported, skipped, fmt.Errorf("flush final batch: %w", err)
	}
	return imported, skipped, nil
}
// autoSync is hot-reload friendly: it loops forever, checking cfg.Sync.ServerURL
// on each iteration. When unset, it sleeps for 60s and re-checks; when set,
// it runs a sync and sleeps for cfg.Sync.AutoIntervalS seconds. Returns only
// on ctx cancellation.
func autoSync(ctx context.Context, cfg *config.Config, m *metrics.Metrics) {
	const disabledPoll = 60 * time.Second
	lastServer := ""
	for {
		if cfg.Sync.ServerURL == "" {
			if !syncSleep(ctx, disabledPoll) {
				return
			}
			continue
		}
		if cfg.Sync.ServerURL != lastServer {
			interval := time.Duration(cfg.Sync.AutoIntervalS) * time.Second
			if interval <= 0 {
				interval = 10 * time.Minute
			}
			slog.Info("auto-sync enabled", "server", cfg.Sync.ServerURL, "interval", interval)
			lastServer = cfg.Sync.ServerURL
		}
		start := time.Now()
		if err := syncBoth(ctx, m); err != nil {
			slog.Warn("auto-sync failed", "err", err)
		}
		if m != nil {
			m.SyncDurationSeconds.WithLabelValues("auto").Observe(time.Since(start).Seconds())
		}
		interval := time.Duration(cfg.Sync.AutoIntervalS) * time.Second
		if interval <= 0 {
			interval = 10 * time.Minute
		}
		if !syncSleep(ctx, interval) {
			return
		}
	}
}

func syncSleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
