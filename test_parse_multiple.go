// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package lth

import (
	"fmt"
	"github.com/mattdurham/lth/internal/watcher"
)

func TestParseMultipleFiles() {
	// Test case: assistant message with TWO Read blocks
	line := []byte(`{"type":"assistant","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/content/_index.md"}},{"type":"tool_use","name":"Read","input":{"file_path":"/content/p1/index.md"}}]}}`)

	paths, sid := watcher.ParseFilePaths(line)
	fmt.Printf("sessionID: %q\n", sid)
	fmt.Printf("paths count: %d\n", len(paths))
	for i, p := range paths {
		fmt.Printf("  path[%d]: %q\n", i, p)
	}
}
