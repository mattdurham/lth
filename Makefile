.PHONY: build test lint ci clean install install-cli install-skill uninstall

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

ci: build test lint

clean:
	rm -rf bin/

# Installation paths (can be overridden: make install GOBIN=/usr/local/bin)
GOBIN ?= $(HOME)/bin
SKILL_DIR ?= $(HOME)/.claude/skills/lth-amnesia

## install: Install lth CLI to ~/bin and lth:amnesia skill to ~/.claude/skills/
install: install-cli install-skill
	@echo ""
	@echo "lth installed successfully."
	@echo "  CLI:   $(GOBIN)/lth"
	@echo "  Skill: $(SKILL_DIR)/SKILL.md"
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

## install-skill: Install lth:amnesia Claude Code skill to ~/.claude/skills/
install-skill:
	@mkdir -p $(SKILL_DIR)
	cp skills/lth-amnesia/SKILL.md $(SKILL_DIR)/SKILL.md
	@echo "✓ lth:amnesia skill installed to $(SKILL_DIR)/SKILL.md"

## uninstall: Remove lth CLI and skill
uninstall:
	rm -f $(GOBIN)/lth
	rm -rf $(SKILL_DIR)
	@echo "✓ lth uninstalled"
