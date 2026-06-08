# internal/watcher — Test Scenarios

## TestParseLine

**Scenario:** Table-driven tests for JSONL parsing.

**Cases:**
- Valid user message with string content → extracted
- queue-operation type → skipped
- System role → skipped  
- Content as text block array → joined text blocks
- Tool result > 10KB → skipped
- Empty content → skipped

## TestExtractContent

**Scenario:** Table-driven tests for content extraction.

**Cases:**
- Plain string content → returned as-is
- Array with one text block → text returned
- Array with multiple text blocks → joined with newline
- Array with tool_result < 10KB → included
- Array with tool_result > 10KB → excluded

## TestWatcherIngestsNewLines

**Scenario:** Write JSONL to temp file; ingest; verify memories stored.

**Setup:** Temp dir, mock store, write JSONL lines.

**Assertions:** `store.Store` called with extracted content.

## TestWatcherOffsetPersistence

**Scenario:** Ingest partial file; restart from saved offset; only new lines ingested.

**Setup:** Write 5 lines; ingest; write 5 more; create new watcher from saved state; ingest.

**Assertions:** Only the 5 new lines are ingested in the second pass.

## TestDetectFormat

**Scenario:** Table-driven tests for format detection by file path.

**Cases:**
- Path containing `/.pi/agent/sessions/` → `FormatPi`
- Path containing `/.wllr/` → `FormatWllr`
- Path containing `/.claude/` → `FormatClaude`
- Other path → `FormatClaude`
- Empty path → `FormatClaude`
- Path under `/.pi/agent/skills/` (no `sessions/`) → `FormatClaude`

## TestDetectFormatPi / TestParsePiLine / TestExtractPiFilePaths

**Scenario:** Table-driven tests for pi agent JSONL parsing.

**ParsePiLine cases:**
- Session header line → `skip=true`, sessionID + cwd returned
- User message with text block → `skip=false`, text returned
- Assistant message mixing `thinking` and `text` blocks → only `text` returned
- Assistant message containing only `toolCall` blocks → `skip=true` (no text content)
- `toolResult` role with text block → `skip=false`, content returned
- `system` role → `skip=true`
- `model_change` / `thinking_level_change` top-level types → `skip=true`
- Empty content array → `skip=true`
- Plain-string `content` (defensive) → `skip=false`, string returned
- Invalid JSON → `skip=true`, err non-nil

**ExtractPiFilePaths cases:**
- Assistant `read` toolCall with `path` → path returned
- Mixed `write` + `edit` + `bash` toolCalls → only write/edit paths returned
- User-role line → nil
- Session-type line → nil
- toolCall with missing `path` arg → nil

## TestParseWllrLine

**Scenario:** Table-driven tests for wllr JSONL line parsing.

**Setup:** Various wllr JSONL line strings with different types and roles.

**Cases:**
- Session line with cwd → `skip=true`, cwd returned from JSON
- Session line with empty cwd → `skip=true`, empty cwd returned
- User message with string content → `skip=false`, content returned, cwd empty (caller uses carriedCWD)
- Assistant message with string content → `skip=false`, content returned
- System role message → `skip=true`
- Unknown role message → `skip=true`
- Empty content message → `skip=true`
- `tool_call` type → `skip=true`
- Unknown type → `skip=true`
- Invalid JSON → `skip=true`, err non-nil
- Message with text block array content → `skip=false`, text extracted
- Message with `cwd` field on the line → field ignored; caller's `carriedCWD` is used

**Assertions:** `skip`, `content`, `cwd`, `sessionID` match expected values per case.

## TestParseWllrLineCarriedCWD

**Scenario:** Simulate real caller pattern — parse session line to get cwd, pass as carriedCWD to message line.

**Setup:** Session line with `cwd="/home/user/myproject"`, followed by a user message line.

**Assertions:**
- Session line: `skip=true`, returned `cwd == "/home/user/myproject"`
- Message line (with session cwd as carriedCWD): `skip=false`, `content == "what does lth do?"`
- `ParseWllrLine` returns empty cwd for message lines — caller uses `carriedCWD` directly for attrs

## TestWatcherIngestsWllrFile

**Scenario:** Integration test — ingest a wllr JSONL file; verify correct memories stored with cwd from session header.

**Setup:**
- Temp dir path containing `/.wllr/` so `detectFormat` returns `FormatWllr`
- JSONL file with: session line (`cwd="/home/user/project"`), user message, assistant message, tool_call line

**Assertions:**
- `store.Store` called exactly 2 times (user + assistant messages only)
- Session line not stored as a memory
- `tool_call` line not stored as a memory
- Both stored memories have `attrs["cwd"] == "/home/user/project"` (carried from session header)
- Content matches the user and assistant message text
