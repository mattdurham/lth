## 1. XML <patch> tag extraction

*Added: 2026-05-15*

**Decision:** Ask Claude to wrap the unified diff in `<patch>...</patch>` XML tags; extract with `strings.Index`.

**Rationale:** Claude reliably follows XML-tag instructions. Regex scanning raw output is fragile because Claude wraps patches in explanation text. `strings.Index` is simpler and more robust than a regex here.

**Consequence:** If Claude outputs a patch without tags, the outcome is `no_patch`. Prompt templates must always include the `<patch>...</patch>` instruction.
