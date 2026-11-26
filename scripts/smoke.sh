#!/usr/bin/env bash
# Smoke test: install the test source into throwaway folders, ready to try with real harnesses.
# Run after `make build`, from the repository root. It never touches the real setup.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
livery="$root/livery"
[ -x "$livery" ] || { echo "smoke: $livery is missing; run: make build" >&2; exit 3; }

command -v jq >/dev/null || { echo "smoke: jq is missing; install jq and run again" >&2; exit 3; }

T=$(mktemp -d)

# The source: the e2e test source as a git repository with two releases: the copy as it is
# (v0.1.0), then a second commit (v0.2.0) that changes an MCP server, an instruction and a skill.
cp -r "$root/e2e/testdata/source" "$T/source"
(
  export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
  export GIT_AUTHOR_NAME=smoke GIT_AUTHOR_EMAIL=smoke@example.invalid
  export GIT_COMMITTER_NAME=smoke GIT_COMMITTER_EMAIL=smoke@example.invalid
  cd "$T/source"
  git init -q -b main
  git add -A
  git commit -q -m "release 0.1.0"
  git tag v0.1.0
  # No `sed -i`: its syntax differs between GNU and BSD (macOS).
  sed 's#https://docs.example.com/mcp#https://docs2.example.com/mcp#' base/mcp/docs.json > base/mcp/docs.json.new
  mv base/mcp/docs.json.new base/mcp/docs.json
  printf '\nPrefer one logical change per commit.\n' >> base/instructions/commits.md
  mkdir base/skills/smoke
  printf -- '---\nname: smoke\ndescription: Added by the second smoke release.\n---\n\nSay that the smoke skill works.\n' > base/skills/smoke/SKILL.md
  git add -A
  git commit -q -m "release 0.2.0"
  git tag v0.2.0
)

export XDG_CONFIG_HOME="$T/config" XDG_CACHE_HOME="$T/cache"
export KIRO_HOME="$T/kiro" CLAUDE_CONFIG_DIR="$T/claude"
mkdir -p "$KIRO_HOME" "$CLAUDE_CONFIG_DIR"

"$livery" setup --source "$T/source" --channel v0.1.0 --team web --harness kiro --harness claude --harness opencode
"$livery" doctor

# Outside writes: every path the lock records must be below the throwaway folder.
lock="$XDG_CONFIG_HOME/livery/lock.json"
[ -f "$lock" ] || lock=$(find "$XDG_CONFIG_HOME" -name lock.json | head -n 1)
[ -n "$lock" ] && [ -f "$lock" ] || { echo "smoke: no lock file below $T/config" >&2; exit 1; }
n=$(jq '.files | length' "$lock")
[ "$n" -gt 0 ] || { echo "smoke: the lock $lock has no files" >&2; exit 1; }
outside=$({ jq -r '.files | keys[]' "$lock"; jq -r '.merged[]?.path' "$lock"; } | grep -v "^$T/" || true)
if [ -n "$outside" ]; then
  echo "smoke: the lock records paths outside $T:" >&2
  echo "$outside" >&2
  exit 1
fi

# The same source as a project, in a second throwaway folder. --project ignores KIRO_HOME,
# CLAUDE_CONFIG_DIR and XDG_CONFIG_HOME; the source cache stays in XDG_CACHE_HOME.
mkdir -p "$T/project"
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git -C "$T/project" init -q -b main
(cd "$T/project" && "$livery" setup --project --source "$T/source" --channel v0.1.0 --team web --harness kiro --harness claude --harness opencode)
(cd "$T/project" && "$livery" doctor --project)
[ -f "$T/project/.livery/lock.json" ] || { echo "smoke: no project lock below $T/project" >&2; exit 1; }

for h in kiro-cli claude opencode; do
  if command -v "$h" >/dev/null 2>&1; then
    echo "$h: $("$h" --version 2>&1 | head -n 1)"
  else
    echo "$h: not installed"
  fi
done

cat <<OUT

Throwaway folder: $T
Claude Code config file: $T/claude/.claude.json (CLAUDE_CONFIG_DIR is set, so it is not ~/.claude.json)

In each new terminal, first set the same environment (livery then uses the throwaway
config and lock, not your real ones):
  export XDG_CONFIG_HOME=$T/config XDG_CACHE_HOME=$T/cache KIRO_HOME=$T/kiro CLAUDE_CONFIG_DIR=$T/claude

Project folder: $T/project (set up with "livery setup --project"; the livery commands there need
"--project" and the same XDG_CACHE_HOME). To try the harnesses there:
  cd $T/project && kiro-cli     (or claude, or opencode)

Then start the harnesses (one terminal each):
  kiro-cli
  claude
  opencode

Checklist (put pass or fail and the three version lines in the release notes you keep; see docs/release.md, "Smoke test"):
  1. The skill "review" is listed.
  2. An instruction with a glob applies to a matching file; an always-on one is loaded.
  3. The harness lists the two MCP servers from the source (docs, github) and reports the missing
     GITHUB_TOKEN variable or a failed start of github-mcp, not a config error. A server actually
     starting is not covered by this smoke test.
     Then, in each harness that can, disable the server docs or approve one of its tools (or add
     "disabled": true to its entry by hand). After that
       "$livery" doctor
     must exit 0 and print no "edited" finding.
  4. Update, all harnesses: with the harness open, run (exported environment above)
       "$livery" setup --channel stable
     The output must show "set" for mcpServers.docs and "write" lines for commits.md and the
     new skill smoke. Then in the harness: the skill "smoke" is listed, the new sentence of
     commits.md ("Prefer one logical change per commit.") is in effect in a new session, and
     (Claude Code) $T/claude/.claude.json kept its other keys. The key the harness added to
     the server docs in 3 is still in its entry.
  5. opencode (config folder $T/config/opencode; the minimum version is in docs/reference.md):
     the skill "review" is listed; the always-on instruction from the "instructions" glob
     (livery/instructions/*.md) is loaded; the MCP servers docs and github are listed (under
     mcp.servers in opencode.json; note the opencode version if it ignores them); after the
     update in 4, the skill "smoke" is listed and commits.md has the new sentence; after
       "$livery" uninstall
     a key you added by hand to opencode.json is still there.
  6. In the project folder the harness lists the skill and both MCP servers.
  7. Guided setup, on a real terminal (exported environment above). uninstall keeps the config
     file and setup asks nothing when it exists, so remove the config too:
       "$livery" uninstall
       rm "$XDG_CONFIG_HOME/livery/config.json"
     then run plain setup, with no flags:
       "$livery" setup
     Answer the questions (the source is $T/source), answer n at "Apply?" and see "nothing
     written". Run the same command again and answer y.
  8. Edited item: change one line of an installed instruction file by hand, then run
       "$livery" update
     It must exit 1, print "edited by you" and an "edited" line that names a file in the
     personal layer, and change nothing else. Follow that line (write the file, remove the
     installed one, run the update): exit 0, your text is in the harness, and
       "$livery" list --installed
     shows the instruction with the layer personal.

When done:
rm -rf $T
OUT
