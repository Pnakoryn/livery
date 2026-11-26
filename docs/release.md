# Cut a release

Only the maintainer can create `v*` tags, and a tag is what makes a release. The history of livery
starts at 0.1.0.

## Steps

1. Check out `main` with a clean tree and pull. Every change on `main` came through a pull request
   with green CI (squash merge).
2. Run `make check`. It must pass.
3. Do the smoke test (below) on that commit, by hand.
4. Tag it and push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.

## What the workflow does

Pushing the tag starts `.github/workflows/release.yml`. It runs `make check`, builds the binaries
with `make dist` (`livery-X.Y.Z-linux-amd64`, `-linux-arm64`, `-darwin-amd64`, `-darwin-arm64` and
`SHA256SUMS`; the version in the names comes from the tag), creates a build provenance
attestation for each file and creates the release at
`https://github.com/Pnakoryn/livery/releases` with the files attached.

## Check the result

Download a file and `SHA256SUMS` from the release, then:

```sh
sha256sum -c --ignore-missing SHA256SUMS
gh attestation verify livery-X.Y.Z-linux-amd64 --repo Pnakoryn/livery
chmod +x livery-X.Y.Z-linux-amd64 && ./livery-X.Y.Z-linux-amd64 version   # livery X.Y.Z
```

Also check that `go install github.com/Pnakoryn/livery/cmd/livery@vX.Y.Z` followed by
`livery version` prints `X.Y.Z`.

## When the workflow fails

First re-run the failed job when the cause was outside the repository (a runner or network
failure): the tag stays where it is. A pushed tag is public and must never move, and it cannot be
deleted from the release history. When the cause is in the repository, fix the problem on `main` through a pull request and release the next patch version
(`vX.Y.Z+1`); do the smoke test again if the fix touches what it covers.

## Smoke test

Run `scripts/smoke.sh` after `make build` (needs `git` and `jq`). It builds a shared repository with two
tags from the e2e test source (`v0.1.0` as it is; `v0.2.0` changes the `docs` MCP server URL,
appends a sentence to `commits.md` and adds the skill `smoke`), installs `v0.1.0` into throwaway
folders (`KIRO_HOME`, `CLAUDE_CONFIG_DIR`, `XDG_*`), runs `livery doctor`, and fails unless every
path in the lock file is below the throwaway folder. It installs for Kiro, Claude Code and opencode (`$T/config/opencode`), and prints the
`kiro-cli --version`, `claude --version` and `opencode --version` lines (or `not installed`), the folder, the two commands to start the
harnesses and the path of Claude Code's config file (`$T/claude/.claude.json`). Your real login
is kept because the home folder is not replaced. Kiro IDE and Kiro CLI read the same folder, so
either can be used.

Start each harness as printed, then check and note pass or fail:

1. The skill `review` is listed. Kiro: pass / fail ___ Claude Code: pass / fail ___ opencode: pass / fail ___
2. An instruction with a glob applies to a matching file, and an always-on one is loaded.
   Kiro: pass / fail ___ Claude Code: pass / fail ___ opencode: pass / fail ___
3. The harness lists the two MCP servers from the shared repository (`docs`, `github`) and reports the
   missing `GITHUB_TOKEN` variable or a failed start of `github-mcp`, not a config error. A
   server actually starting is not covered by this smoke test. Then disable the server `docs`
   or approve one of its tools in the harness (or add `"disabled": true` to its entry by hand):
   `livery doctor` must exit 0 without an `edited` finding.
   Kiro: pass / fail ___ Claude Code: pass / fail ___ opencode: pass / fail ___
4. Update, all harnesses: with the harness open, run `livery setup --channel stable` (same
   environment, the binary from the repository). The output must show `set` for
   `mcpServers.docs` and `write` lines for `commits.md` and the new skill. Then in the harness:
   the skill `smoke` is listed, the new sentence of `commits.md` is in effect in a new session,
   and (Claude Code) the config file kept its other keys. The key the harness added to `docs`
   in 3 is still in its entry.
   Kiro: pass / fail ___ Claude Code: pass / fail ___ opencode: pass / fail ___

5. opencode only: the always-on instruction comes through the `instructions` glob
   (`livery/instructions/*.md`); the MCP servers `docs` and `github` are listed (livery writes
   them under `mcp.servers`; note the opencode version if it ignores them); after the update in
   4 the skill `smoke` is listed; after `livery uninstall` a key you added by hand to
   `opencode.json` is still there. pass / fail ___

6. In the project folder the harness lists the skill and both MCP servers.
   Kiro: pass / fail ___ Claude Code: pass / fail ___ opencode: pass / fail ___

7. Guided setup, on a real terminal (same environment): run `livery uninstall`, then remove
   the config file it keeps (`rm "$XDG_CONFIG_HOME/livery/config.json"`; with a config, setup
   asks nothing). Run plain `livery setup` with no flags and answer the questions (the source is
   `$T/source`, `$T` is the throwaway folder the script printed). Answer `n` at `Apply?` and see
   `nothing written`; run the same command again and answer `y`. pass / fail ___

8. Edited item: change one line of an installed instruction file by hand and run
   `livery update`: exit 1, an `edited` block whose `to keep your change` line names a file in
   the personal layer, nothing else changed. Follow that line: exit 0, your text is in the harness, and
   `livery list --installed` shows the instruction with the layer `personal`. pass / fail ___

Note the results and the three version lines where you track the release. Then run the `rm -rf` line the
script printed.

## Refreshing the output blocks in onboarding.md

The `text` blocks in [onboarding.md](onboarding.md) are the output of the built binary. When the
output format changes, replay them in a throwaway home (never your real one) and paste the new output:

Run it from the livery checkout. It sets a git identity and a throwaway home, so your own
`~/.gitconfig` and `~/.claude` are neither read nor changed. The output of each command is the
block named in the comment; the folder names in the blocks differ.

```sh
make build; L=$PWD/livery
T=$(mktemp -d)
export HOME=$T/home XDG_CONFIG_HOME=$T/xdg-config XDG_CACHE_HOME=$T/xdg-cache XDG_STATE_HOME=$T/xdg-state
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
# your own ~/.claude: two skills, one rule, one MCP server with a token
for s in review deploy; do
  mkdir -p "$HOME/.claude/skills/$s"
  printf -- '---\nname: %s\ndescription: The %s skill\n---\nRead the diff first.\n' $s $s > "$HOME/.claude/skills/$s/SKILL.md"
done
mkdir -p "$HOME/.claude/rules"; printf 'Keep the design simple.\n' > "$HOME/.claude/rules/design.md"
printf '{"mcpServers":{"github":{"command":"gh-mcp","env":{"GITHUB_TOKEN":"ghp_realvalue0123456789"}}}}\n' > "$HOME/.claude.json"
# Import, Check, Publish
$L import --from claude --into $T/team-setup --only skill/review --only skill/deploy --only mcp/github --plan
$L import --from claude --into $T/team-setup --only skill/review --only skill/deploy --only mcp/github
$L check $T/team-setup
git -C $T/team-setup init -b main && git -C $T/team-setup add -A && git -C $T/team-setup commit -m "first release" && git -C $T/team-setup tag v0.1.0
# Switch your own machine: the first block
$L setup --source $T/team-setup --harness claude
# delete the github entry (your version), then the update block and doctor
printf '{}\n' > "$HOME/.claude.json"
$L update; $L doctor
# Change something later: a commit after the tag, then the "next" block
printf 'Ask for tests.\n' >> $T/team-setup/base/skills/review/SKILL.md
git -C $T/team-setup commit -qam "review skill: ask for tests"
$L setup --channel next
$L setup --channel stable
# Add a team later: the write block; then a release for the project block
$L import --from claude --into $T/team-setup --team web --only instruction/design
git -C $T/team-setup add -A && git -C $T/team-setup commit -qm "team web" && git -C $T/team-setup tag v0.1.1
# A project: the last block
mkdir $T/proj && git -C $T/proj init -q -b main && cd $T/proj
$L setup --project --source $T/team-setup --harness claude --team web
```

The "same item in two teams" line needs a second team with the same item: import `design` under
`--team api` too, commit, tag, and run `$L setup --source $T/team-setup --team web --team api`.
Say in the pull request that the blocks were produced by the built binary, and list the commands you ran.
