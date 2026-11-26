# livery

[![CI](https://github.com/Pnakoryn/livery/actions/workflows/ci.yml/badge.svg)](https://github.com/Pnakoryn/livery/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Pnakoryn/livery)](https://github.com/Pnakoryn/livery/releases)
[![License](https://img.shields.io/github/license/Pnakoryn/livery)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/Pnakoryn/livery)](go.mod)

livery is a command-line tool that installs and updates your team's shared coding-agent setup on your machine: skills,
instructions and MCP servers. The setup lives in one git repository, the **shared repository**,
which everyone who uses it reads from. (The words livery uses, such as source, release and layer,
are in [Words](docs/reference.md#words).) livery copies a tagged release of it to the folders your
**harness** reads (the coding-agent program: Kiro, IDE and CLI, Claude Code or opencode). It
records which files it owns and never touches the rest, so your own additions stay on top.

livery never:

- overwrites a file of yours: a file it did not write is reported as `kept, yours` (a file that
  is byte for byte what livery would write is taken over as installed);
- follows a symbolic link, so it changes no file in a place it does not know (one exception: on
  a machine the harness folder itself may be a link, see
  [docs/developers.md](docs/developers.md#symbolic-links));
- uses the network, except to fetch the shared repository with git.

Starting a shared repository from a setup that already works? See
[docs/onboarding.md](docs/onboarding.md).

## Install

Linux and macOS. The tests run on macOS in CI, but a downloaded macOS binary has not been tried
on a Mac by hand yet. If macOS refuses to start a downloaded binary, download it with `curl`
instead of the browser, or remove the quarantine flag with
`xattr -d com.apple.quarantine ~/.local/bin/livery`.

**Download a release.** Every release on
[github.com/Pnakoryn/livery/releases](https://github.com/Pnakoryn/livery/releases) has
`livery-<version>-linux-amd64`, `-linux-arm64`, `-darwin-amd64`, `-darwin-arm64` and `SHA256SUMS`.
(This is not the address of the shared repository, which you pass as `--source`.) The file names
contain the version, so use the address of the release you want, here `0.1.0` on Linux with amd64:

```sh
v=0.1.0
f=livery-$v-linux-amd64
base=https://github.com/Pnakoryn/livery/releases/download/v$v
curl -fsSLO $base/$f
curl -fsSLO $base/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS      # on macOS: shasum -a 256 -c --ignore-missing SHA256SUMS
chmod +x $f
mkdir -p ~/.local/bin
mv $f ~/.local/bin/livery
livery version
```

Each release also has a build provenance attestation. To check it with the
[GitHub CLI](https://cli.github.com/): `gh attestation verify livery-<version>-linux-amd64 --repo Pnakoryn/livery`.

`~/.local/bin` must be on your `PATH`; if `livery version` is not found, add it in your shell's
startup file (`export PATH="$HOME/.local/bin:$PATH"`).

**Or with Go**: `go install github.com/Pnakoryn/livery/cmd/livery@latest` (needs Go 1.27 or newer); then
`livery version` prints the module version, for example `0.1.0`.

Or build it yourself with `make build`; this needs Go (see `mise.toml`) and writes `./livery`.
livery needs `git` on your `PATH`; it runs git to fetch the shared repository and does nothing
else on the network.

## Quick start

The first command installs from
[livery-example](https://github.com/Pnakoryn/livery-example), a small shared repository (tags
`v0.1.0` and `v0.2.0`, a `base/` layer and the team `web`, for Claude Code and Kiro) to try livery
with. A team replaces that address by the address of its own shared repository (a git URL or a
folder); ask your team for it. livery never lets git ask for a password: over HTTPS a credential helper is needed, an SSH address works with an
agent or key.

To see what would be installed first, add `--plan`. It writes nothing except a copy of the
repository in the cache:

```console
$ livery setup --source https://github.com/Pnakoryn/livery-example --plan
config  would save /home/you/.config/livery/config.json: {"source":"https://github.com/Pnakoryn/livery-example","channel":"stable","teams":[],"harnesses":["kiro","claude"]}
livery setup: https://github.com/Pnakoryn/livery-example v0.2.0 (676f3bede2ba)
set     ~/.claude.json mcpServers.context7
set     ~/.claude.json mcpServers.github
write   ~/.claude/agents/reviewer.md
write   ~/.claude/rules/commits.md
write   ~/.claude/rules/github.md
write   ~/.claude/rules/review.md
write   ~/.claude/rules/testing.md
write   ~/.claude/skills/changelog/SKILL.md
write   ~/.claude/skills/changelog/scripts/changes.sh
write   ~/.claude/skills/review-pr/SKILL.md
write   ~/.kiro/agents/reviewer.json
set     ~/.kiro/settings/mcp.json mcpServers.context7
set     ~/.kiro/settings/mcp.json mcpServers.github
write   ~/.kiro/skills/changelog/SKILL.md
write   ~/.kiro/skills/changelog/scripts/changes.sh
write   ~/.kiro/skills/review-pr/SKILL.md
write   ~/.kiro/steering/commits.md
write   ~/.kiro/steering/github.md
write   ~/.kiro/steering/review.md
write   ~/.kiro/steering/testing.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
plan    nothing was written; run the same command without --plan to apply
```

(The real output also shows a diff of every file after this list; it is left out here.) The
shared repository can install MCP servers, hooks and scripts that your harness runs as you, so
read the plan before the first install. Then install:

```console
$ livery setup --source https://github.com/Pnakoryn/livery-example
livery setup: https://github.com/Pnakoryn/livery-example v0.2.0 (676f3bede2ba)
set     ~/.claude.json mcpServers.context7
set     ~/.claude.json mcpServers.github
write   ~/.claude/agents/reviewer.md
write   ~/.claude/rules/commits.md
write   ~/.claude/rules/github.md
write   ~/.claude/rules/review.md
write   ~/.claude/rules/testing.md
write   ~/.claude/skills/changelog/SKILL.md
write   ~/.claude/skills/changelog/scripts/changes.sh
write   ~/.claude/skills/review-pr/SKILL.md
write   ~/.kiro/agents/reviewer.json
set     ~/.kiro/settings/mcp.json mcpServers.context7
set     ~/.kiro/settings/mcp.json mcpServers.github
write   ~/.kiro/skills/changelog/SKILL.md
write   ~/.kiro/skills/changelog/scripts/changes.sh
write   ~/.kiro/skills/review-pr/SKILL.md
write   ~/.kiro/steering/commits.md
write   ~/.kiro/steering/github.md
write   ~/.kiro/steering/review.md
write   ~/.kiro/steering/testing.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   ~/.config/livery/config.json
done    20 change(s) applied
```

The block shows a run without a terminal, where `setup` never asks: it used the harnesses whose
folder exists (`~/.kiro` and `~/.claude` here) and no team. In a terminal, with only `--source`,
`livery setup` asks which coding agents and which teams to set up (Enter takes what it found),
inside a git repository first whether this is for the machine or the project, and `Apply?` before
it writes anything. Without a terminal it needs `--source`. The `note` line means a server needs a
variable you set yourself, here `GITHUB_TOKEN` for GitHub's MCP server; see
[docs/developers.md](docs/developers.md#variables-of-mcp-servers).

When the team publishes a new release, `livery update` installs it. To see that with the example,
install its first release and then follow releases again:

```console
$ livery setup --channel v0.1.0
$ livery setup --channel stable
livery setup: https://github.com/Pnakoryn/livery-example v0.1.0 -> v0.2.0 (676f3bede2ba)
write   ~/.claude/rules/commits.md
write   ~/.claude/skills/changelog/SKILL.md
write   ~/.claude/skills/changelog/scripts/changes.sh
write   ~/.kiro/skills/changelog/SKILL.md
write   ~/.kiro/skills/changelog/scripts/changes.sh
write   ~/.kiro/steering/commits.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   ~/.config/livery/config.json
to go back: livery setup --channel v0.1.0
done    6 change(s) applied
```

To go back to the old release, use the `to go back` command; to remove what livery installed, run
`livery uninstall` (add `--plan` to look first).

Check the result at any time. A line that starts with `finding` usually has a fix after `->` (some
messages name the fix themselves), and `doctor` then exits with 1. What every line word means is
in [Words in the output](docs/reference.md#words-in-the-output):

```console
$ livery doctor
ok       git: /usr/bin/git
ok       config: /home/you/.config/livery/config.json
ok       lock: /home/you/.config/livery/lock.json
ok       harness kiro: ~/.kiro
ok       harness claude: ~/.claude
ok       source: https://github.com/Pnakoryn/livery-example (channel stable)
ok       installed: v0.2.0 (676f3bede2ba)
ok       edited: no installed file was changed by hand
ok       kept, yours: nothing of yours is in the way
info     mcp github: GITHUB_TOKEN is not set in this shell -> set it before you start your coding agent
ok       update: up to date
```

## What is shared

One shared repository, three kinds of content, and where each goes for your harness. `~` is your
home directory; [docs/reference.md](docs/reference.md) has the project paths and the variables
that move the folders.

| | Kiro | Claude Code | opencode |
| --- | --- | --- | --- |
| Skills | `~/.kiro/skills/<name>/` | `~/.claude/skills/<name>/` | `~/.config/opencode/skills/<name>/` |
| Instructions (always on, or only for files that match patterns) | `~/.kiro/steering/<name>.md` | `~/.claude/rules/<name>.md` | `~/.config/opencode/livery/instructions/<name>.md`, listed in `instructions` of `opencode.json` (`opencode.jsonc` when that file exists); **file patterns are not supported: the instruction applies to every file** |
| MCP servers | `mcpServers` in `~/.kiro/settings/mcp.json` | `mcpServers` in `~/.claude.json` | `mcp.servers` in `~/.config/opencode/opencode.json` (`opencode.jsonc` when that file exists) |

## Where to read on

- [docs/developers.md](docs/developers.md): everything after the quick start: guided setup,
  releases and going back, your own changes, variables of MCP servers, machine and project, teams,
  undo.
- [docs/maintainers.md](docs/maintainers.md): the layout of the shared repository, releases,
  `livery check`, CI, what you trust.
- [docs/onboarding.md](docs/onboarding.md): start a shared repository from a setup that works.
- [docs/reference.md](docs/reference.md): commands, files, paths per harness, exit codes, limits.
- [CONTRIBUTING.md](CONTRIBUTING.md): build, test and change livery itself.

## Contributing and license

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Report a security problem as
[SECURITY.md](SECURITY.md) describes. livery is licensed under the Apache License 2.0, see
[LICENSE](LICENSE).
