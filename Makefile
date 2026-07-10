.PHONY: build test lint ci clean install install-cli install-skills install-wllr-skills wllr install-server install-daemon uninstall bench-build benchmark bench-eval bench-all

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
WLLR_SKILLS_DIR ?= $(HOME)/.wllr/skills
UNAME_S := $(shell uname -s)
LAUNCHD_LABEL := com.mattdurham.lth
LAUNCHD_PLIST := $(HOME)/Library/LaunchAgents/$(LAUNCHD_LABEL).plist

## install: Install lth CLI to ~/bin, all lth skills, and the daemon service
install: install-cli install-skills install-daemon
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

## wllr: Install all lth skills to ~/.wllr/skills/
wllr: install-wllr-skills

## install-wllr-skills: Install all lth skills to ~/.wllr/skills/
install-wllr-skills:
	@for skill in skills/*/; do \
		name=$$(basename $$skill); \
		mkdir -p $(WLLR_SKILLS_DIR)/$$name; \
		cp $$skill/SKILL.md $(WLLR_SKILLS_DIR)/$$name/SKILL.md; \
		echo "✓ $$name installed to $(WLLR_SKILLS_DIR)/$$name/SKILL.md"; \
	done

## install-daemon: Install the lth daemon service for this OS and (re)start it
install-daemon:
ifeq ($(UNAME_S),Darwin)
	@mkdir -p $(HOME)/Library/LaunchAgents $(HOME)/.lth
	sed -e 's|__LABEL__|$(LAUNCHD_LABEL)|g' \
		-e 's|__LTH_BIN__|$(GOBIN)/lth|g' \
		-e 's|__HOME__|$(HOME)|g' \
		launchd/lth.plist.in > $(LAUNCHD_PLIST)
	@echo "✓ launchd agent installed to $(LAUNCHD_PLIST)"
	-$(GOBIN)/lth watch stop 2>/dev/null || true
	-launchctl bootout gui/$$(id -u) $(LAUNCHD_PLIST) 2>/dev/null || true
	launchctl bootstrap gui/$$(id -u) $(LAUNCHD_PLIST)
	launchctl enable gui/$$(id -u)/$(LAUNCHD_LABEL)
	launchctl kickstart -k gui/$$(id -u)/$(LAUNCHD_LABEL)
	@echo ""
	@echo "lth daemon running. Check status: launchctl print gui/$$(id -u)/$(LAUNCHD_LABEL)"
	@echo "Logs: tail -f $(HOME)/.lth/daemon.log"
else
	@mkdir -p $(HOME)/.config/systemd/user
	cp lth.service $(HOME)/.config/systemd/user/lth.service
	@echo "✓ systemd unit installed"
	-$(GOBIN)/lth watch stop 2>/dev/null || true
	systemctl --user daemon-reload
	systemctl --user enable lth
	systemctl --user restart lth
	@echo ""
	@echo "lth daemon running. Check status: systemctl --user status lth"
	@echo "Logs: journalctl --user -u lth -f"
endif

## install-server: Build lth-server, install to ~/bin, install systemd user service, and (re)start it
install-server:
	@mkdir -p $(GOBIN)
	go build -o $(GOBIN)/lth-server ./cmd/lth-server
	@echo "✓ lth-server installed to $(GOBIN)/lth-server"
	@mkdir -p $(HOME)/.config/lth-server
	@if [ ! -f $(HOME)/.config/lth-server/config.yaml ]; then \
		cp lth-server.service.example $(HOME)/.config/lth-server/config.yaml 2>/dev/null || \
		printf 'port: 8090\nstorage:\n  provider: local\n  local_dir: ~/.lth-server\n' > $(HOME)/.config/lth-server/config.yaml; \
		echo "✓ default config written to ~/.config/lth-server/config.yaml (edit as needed)"; \
	else \
		echo "  config already exists at ~/.config/lth-server/config.yaml"; \
	fi
	@mkdir -p $(HOME)/.config/systemd/user
	cp lth-server.service $(HOME)/.config/systemd/user/lth-server.service
	@echo "✓ systemd unit installed"
	systemctl --user daemon-reload
	systemctl --user enable lth-server
	systemctl --user restart lth-server
	@echo ""
	@echo "lth-server running. Check status: systemctl --user status lth-server"
	@echo "Logs: journalctl --user -u lth-server -f"

## uninstall: Remove lth CLI, skills, and server service
uninstall:
	rm -f $(GOBIN)/lth $(GOBIN)/lth-server
	rm -rf $(SKILLS_DIR)/lth-amnesia $(SKILLS_DIR)/lth-warmup $(SKILLS_DIR)/lth-brief $(SKILLS_DIR)/lth-reflect $(SKILLS_DIR)/lth-work $(SKILLS_DIR)/lth-work-lite
ifeq ($(UNAME_S),Darwin)
	-launchctl bootout gui/$$(id -u) $(LAUNCHD_PLIST) 2>/dev/null || true
	rm -f $(LAUNCHD_PLIST)
else
	-systemctl --user stop lth lth-server 2>/dev/null || true
	-systemctl --user disable lth lth-server 2>/dev/null || true
	rm -f $(HOME)/.config/systemd/user/lth.service $(HOME)/.config/systemd/user/lth-server.service
endif
	@echo "✓ lth uninstalled"
