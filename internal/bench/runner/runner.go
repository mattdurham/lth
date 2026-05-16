// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/bench/dataset"
	"github.com/mattdurham/lth/internal/bench/patcher"
)

// Config holds runtime settings for the Runner.
type Config struct {
	ClaudeTimeout time.Duration // timeout per claude invocation, default 10m
	Model         string        // claude model name, e.g. "claude-sonnet-4-6" (empty = claude default)
}

// Runner invokes claude for one problem × approach at a time.
type Runner struct {
	cfg Config
}

// New returns a Runner with the given config.
func New(cfg Config) *Runner {
	if cfg.ClaudeTimeout == 0 {
		cfg.ClaudeTimeout = 10 * time.Minute
	}
	return &Runner{cfg: cfg}
}

// RunOne runs claude on one problem × approach and returns the Result.
// It never panics or returns an error — all failures are encoded in Result.Outcome.
func (r *Runner) RunOne(ctx context.Context, problem dataset.Problem, approach Approach) Result {
	start := time.Now()

	claudeCtx, cancel := context.WithTimeout(ctx, r.cfg.ClaudeTimeout)
	defer cancel()

	prompt := approach.BuildPrompt(problem)
	claudeOutput, err := r.runClaude(claudeCtx, prompt)
	if err != nil {
		return Result{
			InstanceID:  problem.InstanceID,
			Approach:    string(approach),
			Outcome:     OutcomeClaudeFail,
			DurationSec: time.Since(start).Seconds(),
			Error:       err.Error(),
			StartedAt:   start,
		}
	}

	patch, ok := patcher.ExtractPatch(claudeOutput)
	if !ok {
		return Result{
			InstanceID:  problem.InstanceID,
			Approach:    string(approach),
			Outcome:     OutcomeNoPatch,
			DurationSec: time.Since(start).Seconds(),
			StartedAt:   start,
		}
	}

	return Result{
		InstanceID:  problem.InstanceID,
		Approach:    string(approach),
		Outcome:     OutcomePass,
		ModelPatch:  patch,
		DurationSec: time.Since(start).Seconds(),
		StartedAt:   start,
	}
}

// runClaude invokes `claude -p --dangerously-skip-permissions` with the prompt on stdin.
// CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 is always set so bob:work and lth:work can
// spawn their subagents. If a model is configured, --model is passed to claude.
func (r *Runner) runClaude(ctx context.Context, prompt string) (string, error) {
	args := []string{"-p", "--dangerously-skip-permissions"}
	if r.cfg.Model != "" {
		args = append(args, "--model", r.cfg.Model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1")
	cmd.WaitDelay = 5 * time.Second

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("claude: %w", err)
	}
	return string(out), nil
}
