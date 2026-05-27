// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/spf13/cobra"
)

const lthVersion = "dev"

var (
	exportOutput    string
	exportChunkSize int
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all memories to a ZIP archive of JSONL files",
	RunE:  runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "output ZIP file path (default: lth-export-<timestamp>.zip)")
	exportCmd.Flags().IntVar(&exportChunkSize, "chunk-size", 1000, "records per JSONL chunk file")
	rootCmd.AddCommand(exportCmd)
}

func runExport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	outputPath := exportOutput
	if outputPath == "" {
		ts := strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "-")
		outputPath = fmt.Sprintf("lth-export-%s.zip", ts)
	}

	d, err := db.Open(globalCfg.DB.Path, 0)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	totalMemories, totalEdges, err := exportDB(ctx, d, outputPath, exportChunkSize)
	if err != nil {
		return err
	}
	fmt.Printf("Exported %d memories, %d edges -> %s\n", totalMemories, totalEdges, outputPath)
	return nil
}

// exportDB writes all active memories and edges from d into a ZIP archive at zipPath.
// Returns the total memory and edge counts written.
func exportDB(ctx context.Context, d *db.DB, zipPath string, chunkSize int) (totalMemories, totalEdges int, err error) {
	f, err := os.Create(zipPath)
	if err != nil {
		return 0, 0, fmt.Errorf("create output file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	zw := zip.NewWriter(f)

	// Pre-load all layers so we can write metadata.json first.
	layerOrder := []int{5, 4, 3, 2, 1}
	layerRows := make(map[int][]*db.MemoryRow, len(layerOrder))
	layerCounts := make(map[string]int, len(layerOrder))
	for _, layer := range layerOrder {
		rows, err := d.ListLayer(ctx, layer, true)
		if err != nil {
			zw.Close() //nolint:errcheck
			return 0, 0, fmt.Errorf("list layer %d: %w", layer, err)
		}
		layerRows[layer] = rows
		if len(rows) > 0 {
			layerCounts[fmt.Sprintf("%d", layer)] = len(rows)
		}
	}

	edges, err := d.GetAllEdges(ctx)
	if err != nil {
		zw.Close() //nolint:errcheck
		return 0, 0, fmt.Errorf("get edges: %w", err)
	}

	preTotal := 0
	for _, rows := range layerRows {
		preTotal += len(rows)
	}

	now := time.Now().UTC()
	metadata := exportMetadata{
		LTHVersion:  lthVersion,
		ExportedAt:  now,
		MemoryCount: preTotal,
		EdgeCount:   len(edges),
		ChunkSize:   chunkSize,
		LayerCounts: layerCounts,
	}
	if err := writeZIPJSON(zw, "metadata.json", metadata); err != nil {
		zw.Close() //nolint:errcheck
		return 0, 0, fmt.Errorf("write metadata: %w", err)
	}

	var files []string

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
			return 0, 0, fmt.Errorf("get attributes for layer %d: %w", layer, err)
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
				return 0, 0, fmt.Errorf("write chunk %s: %w", filename, err)
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
				return 0, 0, fmt.Errorf("write edge chunk %s: %w", filename, err)
			}
			files = append(files, filename)
			totalEdges += len(chunk)
			chunkNum++
		}
	}

	manifest := exportManifest{
		ExportedAt:  now,
		ChunkSize:   chunkSize,
		MemoryCount: totalMemories,
		EdgeCount:   totalEdges,
		Files:       files,
	}
	if err := writeZIPJSON(zw, "manifest.json", manifest); err != nil {
		zw.Close() //nolint:errcheck
		return 0, 0, fmt.Errorf("write manifest: %w", err)
	}

	if err := zw.Close(); err != nil {
		return 0, 0, fmt.Errorf("close zip: %w", err)
	}
	return totalMemories, totalEdges, nil
}

func writeMemoryChunk(zw *zip.Writer, filename string, records []exportMemory) error {
	w, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("create zip entry: %w", err)
	}
	for i := range records {
		b, err := json.Marshal(&records[i])
		if err != nil {
			return fmt.Errorf("marshal memory: %w", err)
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}
	return nil
}

func writeEdgeChunk(zw *zip.Writer, filename string, records []exportEdge) error {
	w, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("create zip entry: %w", err)
	}
	for i := range records {
		b, err := json.Marshal(&records[i])
		if err != nil {
			return fmt.Errorf("marshal edge: %w", err)
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return fmt.Errorf("write edge: %w", err)
		}
	}
	return nil
}

func writeZIPJSON(zw *zip.Writer, filename string, v any) error {
	w, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", filename, err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filename, err)
	}
	_, err = w.Write(b)
	return err
}
