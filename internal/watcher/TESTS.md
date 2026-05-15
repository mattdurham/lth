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