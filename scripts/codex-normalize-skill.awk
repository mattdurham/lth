# Normalizes an lth SKILL.md for the Codex runtime.
# Usage: awk -v name=<skill-dir-name> -f codex-normalize-skill.awk SKILL.md
#
# - Rewrites `name:` to the hyphenated directory name (Codex has no `lth:` namespacing).
# - Drops `requires_experimental:` (no Codex equivalent).
# - Inserts a pointer to references/codex-runtime.md after the frontmatter.
# - Drops `export LTH_ACTIVE=1` and rewrites the prose describing it: it relies on a
#   per-file-read hook Codex does not have, so it would be a silent no-op.

NR == 1 && $0 == "---" { print; fm = 1; next }

fm && $0 == "---" {
	fm = 0
	print
	print ""
	print "> **Codex runtime:** read [references/codex-runtime.md](references/codex-runtime.md)"
	print "> before the instructions below, and let it override any execution mechanic"
	print "> here that names another runtime (`Task`, `TaskCreate`/`TaskList`, teammates,"
	print "> agent-teams flags, `Read`/`Edit`/`Write`, slash-command chaining)."
	next
}

fm && /^name:/ { print "name: " name; next }
fm && /^requires_experimental:/ { next }

# No read hook under Codex; `lth read <file>` is the explicit equivalent.
/^export LTH_ACTIVE=1/ { next }
/`LTH_ACTIVE=1` enables automatic lth context injection whenever a file is read\./ {
	sub(/`LTH_ACTIVE=1` enables automatic lth context injection whenever a file is read\./,
	    "Run `~/bin/lth read <filepath>` to get prior memories and file content together.")
	print
	next
}

{ print }
