.PHONY: build test lint ci clean install install-cli install-skills uninstall bench-build benchmark bench-eval bench-all

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

ci: build test lint

## bench-build: Build bench binary to bin/bench
bench-build:
	go build -o bin/bench ./cmd/bench

## benchmark: Run all 42 SWE-bench Multilingual Go problems (pass/fail scored by official harness)
benchmark: bench-build
	./bin/bench run --problems 42 --language go

## bench-eval: Score predictions using the official SWE-bench evaluation harness (requires Docker)
bench-eval:
	@for approach in bob-work lth-work lth-single default; do \
		if [ -f predictions-$$approach.jsonl ]; then \
			echo "=== Evaluating $$approach ==="; \
			python3 -m swebench.harness.run_evaluation \
				--dataset_name SWE-bench/SWE-bench_Multilingual \
				--predictions_path predictions-$$approach.jsonl \
				--run_id $$approach \
				--split test \
				--max_workers 1; \
		else \
			echo "Skipping $$approach: predictions-$$approach.jsonl not found (run make benchmark first)"; \
		fi \
	done

## bench-all: Run inference then evaluate — full end-to-end benchmark
bench-all: benchmark bench-eval

clean:
	rm -rf bin/

# Installation paths (can be overridden: make install GOBIN=/usr/local/bin)
GOBIN ?= $(HOME)/bin
SKILL_DIR ?= $(HOME)/.claude/skills/lth-amnesia
SKILLS_DIR ?= $(HOME)/.claude/skills

## install: Install lth CLI to ~/bin and all lth skills to ~/.claude/skills/
install: install-cli install-skills
	@echo ""
	@echo "lth installed successfully."
	@echo "  CLI:    $(GOBIN)/lth"
	@echo "  Skills: $(SKILLS_DIR)/"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Set ANTHROPIC_API_KEY in your shell profile"
	@echo "  2. Run: lth config init"
	@echo "  3. Run: lth stats   (starts daemon + embedding container)"
	@echo "  4. Seed L1 memories: lth store --layer 1 'your core identity'"
	@echo "  5. Use /lth:amnesia in Claude Code"

## install-cli: Build and install lth binary to ~/bin (or GOBIN=...)
install-cli:
	@mkdir -p $(GOBIN)
	go build -o $(GOBIN)/lth ./cmd/lth
	@echo "✓ lth installed to $(GOBIN)/lth"

## install-skills: Install all lth skills to ~/.claude/skills/
install-skills:
	@for skill in skills/*/; do \
		name=$$(basename $$skill); \
		mkdir -p $(SKILLS_DIR)/$$name; \
		cp $$skill/SKILL.md $(SKILLS_DIR)/$$name/SKILL.md; \
		echo "✓ $$name installed to $(SKILLS_DIR)/$$name/SKILL.md"; \
	done

## uninstall: Remove lth CLI and skills
uninstall:
	rm -f $(GOBIN)/lth
	rm -rf $(SKILLS_DIR)/lth-amnesia $(SKILLS_DIR)/lth-warmup $(SKILLS_DIR)/lth-brief $(SKILLS_DIR)/lth-reflect
	@echo "✓ lth uninstalled"
