// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mattdurham/lth/internal/llm"
	"github.com/spf13/cobra"
)

var (
	tagAutoAttrKey   string
	tagAutoValues    string
	tagAutoLayers    []int
	tagAutoDryRun    bool
	tagAutoOverwrite bool
	tagAutoWithTags  bool
)

var tagAutoCmd = &cobra.Command{
	Use:   "tag-auto",
	Short: "Auto-classify untagged memories using the configured LLM",
	Long: `Classifies memories that are missing a given attribute key and writes
the LLM-assigned value back via MergeAttr. Uses whatever LLM provider is
configured in ~/.lth/config.yaml — no model is hardcoded.

With --with-tags the LLM also generates up to 3 topic tags in the same call,
written as the "tags" attribute (comma-separated, matching the compactor format).

Examples:
  lth tag-auto --attr domain
  lth tag-auto --attr domain --values "coding,email,ops,research,general" --with-tags
  lth tag-auto --attr domain --layers 1 2 --dry-run
  lth tag-auto --attr domain --overwrite --with-tags`,
	RunE: runTagAuto,
}

func init() {
	tagAutoCmd.Flags().StringVar(&tagAutoAttrKey, "attr", "", "attribute key to classify (required)")
	tagAutoCmd.Flags().StringVar(&tagAutoValues, "values", "", "comma-separated list of allowed values; if empty the LLM chooses freely")
	tagAutoCmd.Flags().IntSliceVar(&tagAutoLayers, "layers", []int{1, 2}, "layers to scan (default: 1,2)")
	tagAutoCmd.Flags().BoolVar(&tagAutoDryRun, "dry-run", false, "print classifications without writing them")
	tagAutoCmd.Flags().BoolVar(&tagAutoOverwrite, "overwrite", false, "re-classify memories that already have the attr (default: skip them)")
	tagAutoCmd.Flags().BoolVar(&tagAutoWithTags, "with-tags", false, "also generate up to 3 topic tags in the same LLM call, written as the \"tags\" attr")
	if err := tagAutoCmd.MarkFlagRequired("attr"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(tagAutoCmd)
}

func runTagAuto(cmd *cobra.Command, _ []string) error {
	if globalCfg == nil {
		return fmt.Errorf("config not loaded")
	}

	l := llm.New(globalCfg)
	if l == nil {
		return fmt.Errorf("no LLM configured — set llm.provider in config.yaml")
	}

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	var allowed []string
	if tagAutoValues != "" {
		for _, v := range strings.Split(tagAutoValues, ",") {
			if v = strings.TrimSpace(v); v != "" {
				allowed = append(allowed, v)
			}
		}
	}

	var total, tagged, skipped, failed int

	for _, layer := range tagAutoLayers {
		memories, err := client.ListLayer(cmd.Context(), layer)
		if err != nil {
			return fmt.Errorf("list layer %d: %w", layer, err)
		}

		for _, m := range memories {
			total++
			existing := m.Attrs[tagAutoAttrKey]
			if m.Attrs["lth_classified"] == "1" && !tagAutoOverwrite {
				skipped++
				continue
			}

			attrs, err := classifyMemory(cmd.Context(), l, m.Content, tagAutoAttrKey, allowed, tagAutoWithTags)
			if err != nil {
				fmt.Printf("  FAIL  [L%d] %s: %v\n", layer, m.ID[:8], err)
				failed++
				continue
			}

			action := "tag"
			if existing != "" {
				action = fmt.Sprintf("retag (%s→%s)", existing, attrs[tagAutoAttrKey])
			}

			if tagAutoDryRun {
				parts := make([]string, 0, len(attrs))
				for k, v := range attrs {
					parts = append(parts, k+"="+v)
				}
				fmt.Printf("  dry   [L%d] %s  %s  (%s)\n", layer, m.ID[:8], strings.Join(parts, "  "), action)
			} else {
				writeErr := false
				for k, v := range attrs {
					if err := client.MergeAttr(cmd.Context(), m.ID, k, v); err != nil {
						fmt.Printf("  FAIL  [L%d] %s: write %s: %v\n", layer, m.ID[:8], k, err)
						writeErr = true
					}
				}
				if writeErr {
					failed++
					continue
				}
				// Mark as LLM-classified so subsequent runs skip it unless --overwrite.
				_ = client.MergeAttr(cmd.Context(), m.ID, "lth_classified", "1")
				parts := make([]string, 0, len(attrs))
				for k, v := range attrs {
					parts = append(parts, k+"="+v)
				}
				fmt.Printf("  ok    [L%d] %s  %s  (%s)\n", layer, m.ID[:8], strings.Join(parts, "  "), action)
			}
			tagged++
		}
	}

	fmt.Printf("\ntotal=%d  tagged=%d  skipped=%d  failed=%d", total, tagged, skipped, failed)
	if tagAutoDryRun {
		fmt.Print("  (dry-run, no writes)")
	}
	fmt.Println()
	return nil
}

// classifyMemory asks the LLM to assign attributes for the given memory content.
// Always classifies attrKey (constrained to allowed values when non-empty).
// When withTags is true, also generates up to 3 topic tags returned under "tags".
// Returns a map of attr key→value pairs ready to write.
func classifyMemory(ctx context.Context, l llm.LLM, content, attrKey string, allowed []string, withTags bool) (map[string]string, error) {
	var constraint string
	if len(allowed) > 0 {
		quoted := make([]string, len(allowed))
		for i, v := range allowed {
			quoted[i] = fmt.Sprintf("%q", v)
		}
		constraint = fmt.Sprintf("one of: %s", strings.Join(quoted, ", "))
	} else {
		constraint = "a short lowercase slug (no spaces, no punctuation other than hyphens)"
	}

	var prompt string
	if withTags {
		prompt = fmt.Sprintf(
			"You are classifying a memory entry. Return ONLY a valid JSON object (no markdown) with:\n"+
				"- %q: %s\n"+
				"- \"tags\": array of up to 3 lowercase topic/technology strings most relevant to this memory\n\n"+
				"Memory content:\n%s\n\n"+
				"Example: {%q: \"coding\", \"tags\": [\"mutex\", \"concurrency\", \"go\"]}",
			attrKey, constraint, content, attrKey,
		)
	} else {
		prompt = fmt.Sprintf(
			"You are classifying a memory entry for attribute %q.\nValue must be %s.\n\n"+
				"Memory content:\n%s\n\nReturn ONLY a JSON string — the attribute value. Example: \"coding\"",
			attrKey, constraint, content,
		)
	}

	resp, err := l.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}

	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	if withTags {
		return parseMultiAttrResponse(resp, attrKey, allowed)
	}
	return parseSingleAttrResponse(resp, attrKey, allowed)
}

// parseSingleAttrResponse parses a JSON string response into a single-entry attr map.
func parseSingleAttrResponse(resp, attrKey string, allowed []string) (map[string]string, error) {
	var value string
	if err := json.Unmarshal([]byte(resp), &value); err != nil {
		value = strings.Trim(resp, `"' `)
		if value == "" {
			return nil, fmt.Errorf("empty classification response")
		}
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if err := validateAllowed(value, allowed); err != nil {
		return nil, err
	}
	return map[string]string{attrKey: value}, nil
}

// parseMultiAttrResponse parses a JSON object response containing attrKey + "tags" array.
func parseMultiAttrResponse(resp, attrKey string, allowed []string) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal multi-attr response: %w", err)
	}

	result := make(map[string]string, 2)

	// Parse primary attr.
	if v, ok := raw[attrKey]; ok {
		var primary string
		if err := json.Unmarshal(v, &primary); err != nil {
			return nil, fmt.Errorf("parse %q field: %w", attrKey, err)
		}
		primary = strings.ToLower(strings.TrimSpace(primary))
		if err := validateAllowed(primary, allowed); err != nil {
			return nil, err
		}
		result[attrKey] = primary
	} else {
		return nil, fmt.Errorf("response missing %q field", attrKey)
	}

	// Parse tags array — store as comma-separated string matching compactor format.
	if v, ok := raw["tags"]; ok {
		var tags []string
		if err := json.Unmarshal(v, &tags); err != nil {
			return nil, fmt.Errorf("parse \"tags\" field: %w", err)
		}
		clean := make([]string, 0, len(tags))
		for _, t := range tags {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				clean = append(clean, t)
			}
		}
		if len(clean) > 3 {
			clean = clean[:3]
		}
		if len(clean) > 0 {
			result["tags"] = strings.Join(clean, ",")
		}
	}

	return result, nil
}

// validateAllowed returns an error if value is not in the allowed list (case-insensitive).
// No-ops when allowed is empty.
func validateAllowed(value string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			return nil
		}
	}
	return fmt.Errorf("LLM returned %q which is not in allowed values %v", value, allowed)
}
