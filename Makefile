.PHONY: build test lint ci clean install install-cli install-skills uninstall bench-build benchmark bench-eval

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

## benchmark: Run SWE-bench on 5 Go problems
benchmark: bench-build
	./bin/bench run --problems 5 --language go

## bench-eval: Instructions to run official SWE-bench evaluation
bench-eval:
	@echo "To evaluate predictions, run the official SWE-bench harness:"
	@echo "  pip install swe-bench"
	@echo "  python -m swebench.harness.run_evaluation --predictions_path predictions-lth-work.jsonl --run_id lth-work"

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
