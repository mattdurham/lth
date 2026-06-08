// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package config

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// reloadMu serializes concurrent ReloadInPlace calls and protects in-place
// struct overwrites. Readers of the *Config that goroutines captured at daemon
// startup do not acquire this lock — torn reads of individual fields are
// theoretically possible but in practice extremely rare and never fatal (the
// daemon's per-tick loops simply pick up either the old or new value on a
// given iteration).
var reloadMu sync.Mutex

// HotFields enumerates configuration field paths whose new values are picked
// up by the running daemon without a restart, because the consumer re-reads
// them from the shared *Config on every iteration / per-request.
//
// Field paths not in this set DO take effect after a restart, but the daemon's
// running goroutines have already captured the old value (e.g. as a ticker
// interval baked into time.NewTicker at goroutine start, or as a constructor
// argument). ReloadInPlace reports those in the requiresRestart return value.
//
// Keep this list ordered alphabetically and grouped by section for readability.
var HotFields = map[string]bool{
	// Compaction tuning — read per tick by the compactor.
	"Compaction.L3EpisodesMin":        true,
	"Compaction.L3ImportanceMin":      true,
	"Compaction.L4ClusterSize":        true,
	"Compaction.L5ClusterThreshold":   true,
	"Compaction.L5MaxAgeH":            true,
	"Compaction.L5MinClusterSize":     true,
	"Compaction.L5Threshold":          true,
	"Compaction.SeedMinL2":            true,
	"Compaction.SeedMinL3":            true,
	"Compaction.SeedSample":           true,
	"Compaction.ValenceCompactionMin": true,

	// Search weights — read per query.
	"Search.Alpha":       true,
	"Search.Beta":        true,
	"Search.Gamma":       true,
	"Search.DefaultTopK": true,

	// Sync push/pull endpoints — read at each push/pull invocation.
	"Sync.Account":   true,
	"Sync.Org":       true,
	"Sync.ServerURL": true,
	"Sync.Team":      true,
	"Sync.User":      true,

	// Markdown / Issues watchers — read per tick.
	"Issues.Repos":        true,
	"Markdown.Dirs":       true,
	"Markdown.GitPull":    true,
	"Markdown.Layer":      true,
}

// ReloadInPlace re-reads path, validates the new config, and overwrites dst's
// fields with the new values under reloadMu. Returns the dotted field paths
// that changed and, of those, the ones whose change requires a daemon restart
// to fully take effect (per HotFields above).
//
// If the file does not parse cleanly, dst is left untouched and an error is
// returned. This is the policy that makes "poll and reload" safe: a broken
// edit never kills a running daemon.
func ReloadInPlace(path string, dst *Config) (changed, requiresRestart []string, err error) {
	newCfg, err := Load(path)
	if err != nil {
		return nil, nil, fmt.Errorf("config reload: %w", err)
	}

	reloadMu.Lock()
	defer reloadMu.Unlock()

	changed = diffFields(dst, newCfg, "")
	if len(changed) == 0 {
		return nil, nil, nil
	}

	// Whole-struct overwrite. On 64-bit Go a struct copy is not atomic, but
	// each individual field write (int, float32, string header, slice header)
	// is small enough that concurrent readers see either the old or new value
	// on a given read, never a torn intermediate. The daemon's per-tick loops
	// take the next consistent snapshot on the following iteration.
	*dst = *newCfg

	for _, f := range changed {
		if !HotFields[f] {
			requiresRestart = append(requiresRestart, f)
		}
	}
	sort.Strings(changed)
	sort.Strings(requiresRestart)
	return changed, requiresRestart, nil
}

// diffFields returns the dotted field paths where a and b differ. Reflection
// is fine here because this runs at most once per minute. Unexported fields,
// pointers, and maps are not present in Config so we only need to handle
// structs, primitives, strings, and slices of strings.
func diffFields(a, b *Config, prefix string) []string {
	return diffValues(reflect.ValueOf(*a), reflect.ValueOf(*b), prefix)
}

func diffValues(av, bv reflect.Value, prefix string) []string {
	var out []string
	t := av.Type()
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			fname := t.Field(i).Name
			child := fname
			if prefix != "" {
				child = prefix + "." + fname
			}
			out = append(out, diffValues(av.Field(i), bv.Field(i), child)...)
		}
	case reflect.Slice:
		if !reflect.DeepEqual(av.Interface(), bv.Interface()) {
			out = append(out, prefix)
		}
	default:
		if av.Interface() != bv.Interface() {
			out = append(out, prefix)
		}
	}
	return out
}
