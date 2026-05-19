// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package patcher

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ApplyPatch applies a unified diff patch to repoDir using `git apply --whitespace=fix`.
// Returns a descriptive error including stderr on failure.
func ApplyPatch(ctx context.Context, repoDir, patch string) error {
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=fix", "-")
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply: %w\n%s", err, out)
	}
	return nil
}
