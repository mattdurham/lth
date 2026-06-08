// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/spf13/cobra"
)

var (
	importDryRun       bool
	importSkipExisting bool
)

var importCmd = &cobra.Command{
	Use:   "import <zipfile>",
	Short: "Import memories from a ZIP archive produced by lth export",
	Args:  cobra.ExactArgs(1),
	RunE:  runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "validate records without writing to the database")
	importCmd.Flags().BoolVar(&importSkipExisting, "skip-existing", false, "skip duplicate memories and edges instead of failing")
	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	zipPath := args[0]

	var d *db.DB
	if !importDryRun {
		var err error
		d, err = db.Open(globalCfg.DB.Path, 0)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer d.Close() //nolint:errcheck
	}

	importedMemories, importedEdges, skipped, err := importDB(ctx, d, zipPath, importDryRun, importSkipExisting)
	if err != nil {
		return err
	}

	if importDryRun {
		if zr, zipErr := zip.OpenReader(zipPath); zipErr == nil {
			if meta, metaErr := readMetadata(&zr.Reader); metaErr == nil {
				fmt.Printf("Archive: lth_version=%s exported_at=%s memories=%d edges=%d\n",
					meta.LTHVersion, meta.ExportedAt.Format("2006-01-02T15:04:05Z"), meta.MemoryCount, meta.EdgeCount)
			}
			zr.Close() //nolint:errcheck
		}
		fmt.Printf("Dry run: found %d memories, %d edges (no changes made)\n", importedMemories, importedEdges)
	} else {
		fmt.Printf("Imported %d memories, %d edges (%d skipped) -> %s\n", importedMemories, importedEdges, skipped, zipPath)
	}
	return nil
}

// importDB reads the ZIP archive at zipPath and writes its records into d.
// Pass d=nil with dryRun=true to validate without writing.
// Returns counts of imported memories, edges, and skipped duplicates.
func importDB(ctx context.Context, d *db.DB, zipPath string, dryRun, skipExisting bool) (importedMemories, importedEdges, skipped int, err error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close() //nolint:errcheck

	manifest, err := readManifest(&zr.Reader)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, filename := range manifest.Files {
		f := findZipFile(&zr.Reader, filename)
		if f == nil {
			return 0, 0, 0, fmt.Errorf("file %q listed in manifest not found in archive", filename)
		}
		rc, err := f.Open()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open zip entry %q: %w", filename, err)
		}

		switch {
		case strings.HasPrefix(filename, "memories_"):
			n, skip, err := importMemories(ctx, d, rc, dryRun, skipExisting)
			rc.Close() //nolint:errcheck
			if err != nil {
				return importedMemories, importedEdges, skipped, fmt.Errorf("import %s: %w", filename, err)
			}
			importedMemories += n
			skipped += skip

		case strings.HasPrefix(filename, "edges_"):
			n, skip, err := importEdges(ctx, d, rc, dryRun, skipExisting)
			rc.Close() //nolint:errcheck
			if err != nil {
				return importedMemories, importedEdges, skipped, fmt.Errorf("import %s: %w", filename, err)
			}
			importedEdges += n
			skipped += skip

		default:
			rc.Close() //nolint:errcheck
		}
	}
	return importedMemories, importedEdges, skipped, nil
}

func importMemories(ctx context.Context, d *db.DB, rc interface{ Read([]byte) (int, error) }, dryRun, skipExisting bool) (imported, skipped int, err error) {
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
		if em.ID == "" || em.Layer < 1 || em.Layer > 5 || em.Content == "" {
			return imported, skipped, fmt.Errorf("invalid memory record: id=%q layer=%d", em.ID, em.Layer)
		}

		if dryRun {
			imported++
			continue
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
			Source:         em.Source,
			Agent:          em.Agent,
			Valence:        em.Valence,
			ValenceScored:  em.ValenceScored,
		}

		insertErr := d.InsertMemory(ctx, row)
		if insertErr != nil {
			if skipExisting && strings.Contains(insertErr.Error(), "UNIQUE constraint failed") {
				skipped++
				continue
			}
			return imported, skipped, fmt.Errorf("insert memory %q: %w", em.ID, insertErr)
		}

		if len(em.Attrs) > 0 {
			if err := d.SetAttributes(ctx, em.ID, em.Attrs); err != nil {
				return imported, skipped, fmt.Errorf("set attributes for %q: %w", em.ID, err)
			}
		}
		imported++
	}
	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("scan memory lines: %w", err)
	}
	return imported, skipped, nil
}

func importEdges(ctx context.Context, d *db.DB, rc interface{ Read([]byte) (int, error) }, dryRun, skipExisting bool) (imported, skipped int, err error) {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ee exportEdge
		if err := json.Unmarshal(line, &ee); err != nil {
			return imported, skipped, fmt.Errorf("decode edge: %w", err)
		}
		if ee.FromID == "" || ee.ToID == "" || ee.EdgeType == "" {
			return imported, skipped, fmt.Errorf("invalid edge record: from=%q to=%q type=%q", ee.FromID, ee.ToID, ee.EdgeType)
		}

		if dryRun {
			imported++
			continue
		}

		edgeRow := &db.EdgeRow{
			FromID:    ee.FromID,
			ToID:      ee.ToID,
			EdgeType:  ee.EdgeType,
			Weight:    ee.Weight,
			CreatedAt: ee.CreatedAt,
		}
		insertErr := d.InsertEdge(ctx, edgeRow)
		if insertErr != nil {
			if skipExisting && strings.Contains(insertErr.Error(), "UNIQUE constraint failed") {
				skipped++
				continue
			}
			return imported, skipped, fmt.Errorf("insert edge from=%q to=%q type=%q: %w", ee.FromID, ee.ToID, ee.EdgeType, insertErr)
		}
		imported++
	}
	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("scan edge lines: %w", err)
	}
	return imported, skipped, nil
}

func readManifest(zr *zip.Reader) (exportManifest, error) {
	f := findZipFile(zr, "manifest.json")
	if f == nil {
		return exportManifest{}, fmt.Errorf("manifest.json not found in archive")
	}
	rc, err := f.Open()
	if err != nil {
		return exportManifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	var m exportManifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return exportManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return m, nil
}

func readMetadata(zr *zip.Reader) (exportMetadata, error) {
	f := findZipFile(zr, "metadata.json")
	if f == nil {
		return exportMetadata{}, fmt.Errorf("metadata.json not found in archive")
	}
	rc, err := f.Open()
	if err != nil {
		return exportMetadata{}, fmt.Errorf("open metadata: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	var m exportMetadata
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return exportMetadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	return m, nil
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
