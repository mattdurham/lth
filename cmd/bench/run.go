// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/bench/dataset"
	"github.com/mattdurham/lth/internal/bench/predictions"
	"github.com/mattdurham/lth/internal/bench/report"
	"github.com/mattdurham/lth/internal/bench/runner"
	"github.com/spf13/cobra"
)

var (
	flagProblems   int
	flagOffset     int
	flagLanguage   string
	flagApproaches string
	flagTimeout    time.Duration
	flagModel      string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run SWE-bench problems",
	RunE:  runBench,
}

func init() {
	runCmd.Flags().IntVar(&flagProblems, "problems", 5, "number of problems to run")
	runCmd.Flags().IntVar(&flagOffset, "offset", 0, "offset into problem list")
	runCmd.Flags().StringVar(&flagLanguage, "language", "go", "language filter (only 'go' supported)")
	runCmd.Flags().StringVar(&flagApproaches, "approaches", "", "comma-separated approaches (default: all)")
	runCmd.Flags().DurationVar(&flagTimeout, "timeout", 10*time.Minute, "timeout per Claude invocation")
	runCmd.Flags().StringVar(&flagModel, "model", "", "claude model name (default: claude's default)")
	rootCmd.AddCommand(runCmd)
}

func runBench(cmd *cobra.Command, _ []string) error {
	approaches := runner.AllApproaches
	if flagApproaches != "" {
		parts := strings.Split(flagApproaches, ",")
		approaches = make([]runner.Approach, 0, len(parts))
		for _, p := range parts {
			approaches = append(approaches, runner.Approach(strings.TrimSpace(p)))
		}
	}

	ctx := context.Background()
	problems, err := dataset.NewHFClient().FetchProblems(ctx, flagOffset, flagProblems, flagLanguage)
	if err != nil {
		return fmt.Errorf("fetch problems: %w", err)
	}

	writer, err := report.NewWriter(flagResults)
	if err != nil {
		return fmt.Errorf("open results file: %w", err)
	}
	defer writer.Close()

	completed, err := report.LoadCompleted(flagResults)
	if err != nil {
		return fmt.Errorf("load completed: %w", err)
	}

	cfg := runner.Config{
		ClaudeTimeout: flagTimeout,
		Model:         flagModel,
	}

	// Open per-approach prediction writers (lazy, keyed by approach string).
	predWriters := map[string]*predictions.Writer{}
	defer func() {
		for _, pw := range predWriters {
			pw.Close()
		}
	}()
	predWriter := func(approach runner.Approach) (*predictions.Writer, error) {
		key := string(approach)
		if pw, ok := predWriters[key]; ok {
			return pw, nil
		}
		pw, err := predictions.NewWriter(predictions.PredictionsPath(key))
		if err != nil {
			return nil, err
		}
		predWriters[key] = pw
		return pw, nil
	}

	for _, problem := range problems {
		for _, approach := range approaches {
			key := problem.InstanceID + ":" + string(approach)
			if completed[key] {
				fmt.Printf("skipping %s\n", key)
				continue
			}
			fmt.Printf("running %s x %s...\n", problem.InstanceID, approach)
			result := runner.New(cfg).RunOne(ctx, problem, approach)
			if err := writer.AppendResult(result); err != nil {
				return fmt.Errorf("append result: %w", err)
			}
			pw, err := predWriter(approach)
			if err != nil {
				return fmt.Errorf("open predictions file: %w", err)
			}
			if err := pw.Append(predictions.Prediction{
				InstanceID: result.InstanceID,
				ModelPatch: result.ModelPatch,
				ModelName:  string(approach),
			}); err != nil {
				return fmt.Errorf("append prediction: %w", err)
			}
			patchDesc := "no patch"
			if result.ModelPatch != "" {
				patchDesc = fmt.Sprintf("patch: %d bytes", len(result.ModelPatch))
			}
			fmt.Printf("  -> %s (%s, %.1fs)\n", result.Outcome, patchDesc, result.DurationSec)
		}
	}

	allResults, err := loadAllResults(flagResults)
	if err != nil {
		return fmt.Errorf("load results: %w", err)
	}
	report.PrintSummary(allResults, os.Stdout)

	if flagJSON {
		if err := json.NewEncoder(os.Stdout).Encode(allResults); err != nil {
			return fmt.Errorf("encode JSON: %w", err)
		}
	}
	return nil
}

func loadAllResults(path string) ([]runner.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var results []runner.Result
	dec := json.NewDecoder(f)
	for dec.More() {
		var r runner.Result
		if err := dec.Decode(&r); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}
