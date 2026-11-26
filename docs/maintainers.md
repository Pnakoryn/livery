# For maintainers of a shared repository

You keep the git repository that everyone who uses it installs from. To start one from a setup
that already works, read [onboarding.md](onboarding.md) first; this page is the reference for what
the repository holds and how you check and release it. After the first release onboarding.md
goes on with [your own machine](onboarding.md#switch-your-own-machine), [telling the team](onboarding.md#tell-the-team),
[later changes](onboarding.md#change-something-later), [release rules](onboarding.md#release-rules) and
[CI](onboarding.md#ci). To cut a release of livery itself, see
[release.md](release.md).

## Layout of the shared repository

A shared repository is a git repository with this layout. [livery-example](https://github.com/Pnakoryn/livery-example)
is a small one you can copy.

```text
livery.json                  # {"schema": 1}
base/
  skills/<name>/SKILL.md     # Agent Skills, with any files beside it
  instructions/<name>.md     # always-on or file-scoped guidance
  mcp/<name>.json            # one MCP server per file
  kiro/                      # files only Kiro understands
  claude/agents/             # Claude Code subagents, copied as they are
teams/<team>/                # same shape as base/
```

The repository is dedicated to livery (its own `vX.Y.Z` tags): files outside `livery.json`,
`base/` and `teams/` are ignored on install. Files and folders starting with `.` in a layer are
not ignored: a `.DS_Store` inside a skill folder (`base/skills/<name>/.DS_Store`) is installed with
the skill, and `livery check` reports a binary one as `finding  secrets: ...: binary file, not
scanned`. The fix is to remove it (and list `.DS_Store` in the shared repository's `.gitignore`),
or to add the `secrets-allow` line the finding prints. A `.DS_Store` directly in `base/` belongs
to no item: it is not installed, but `check` reports it the same way.

- `livery.json` holds `{"schema": 1}`.
- Names follow two rules. An MCP server (`mcp/<name>.json`) matches
  `^[A-Za-z0-9][A-Za-z0-9._-]*$` (`awslabs.core-mcp-server`, `My_Server`); the name is the JSON key
  in every harness, unchanged. Everything else (skills, instructions, teams) matches
  `^[a-z0-9][a-z0-9-]*$`.
- An instruction is a Markdown file. The frontmatter is optional and has two keys: `description`
  and `globs`. With `globs` (a list of file patterns) the instruction applies to matching files
  only; without it, it is always on. opencode has no file patterns: there an instruction with
  `globs` applies to every file, and `setup`, `update` and `check` say so with a `note` or `info`
  line. `description` is read but not used by any adapter yet.

  ```markdown
  ---
  description: Go style
  globs: ["**/*.go"]
  ---

  Use the standard library first. Wrap errors with context.
  ```
- An MCP file describes one server and uses only these five keys: `command`, `args`, `env`, `url`,
  `headers`. Exactly one of `command` and `url` is required. Never write a secret into it; write
  `${VAR}` and each person sets the variable (livery prints a `note` for a variable that is not
  set, see [developers.md](developers.md#variables-of-mcp-servers)).

  ```json
  {"command": "github-mcp", "args": ["stdio"], "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}
  ```
- `kiro/` holds native Kiro files in `steering/`, `agents/`, `hooks/` and `workflows/`; they are
  copied to the same place below the Kiro folder. Other paths there are refused.
- `claude/` holds Claude Code subagents in `agents/` (one Markdown file with frontmatter each,
  subfolders allowed); they are copied byte for byte to `agents/` below the Claude Code folder,
  and livery does not check their frontmatter. Other paths there are refused. Native files for
  opencode are not supported yet.
- Teams: `teams/<team>/` has the same shape as `base/`. Developers choose them with `--team`; see
  [developers.md](developers.md#teams) for how layers combine and what a clash is.

## Releases and channels

A release is a `vX.Y.Z` tag on the shared repository. A tag should never move to another
commit: `update` and `doctor` say so when one did since a developer's last run. Developers follow
a **channel**:

| Channel | They get |
| --- | --- |
| `stable` (default) | the newest `vX.Y.Z` tag |
| `next` | the newest commit on the default branch |
| `vX.Y.Z` | exactly that release |

Run `livery check` (below) before you tag.

## livery check

`livery check [DIR]` (DIR defaults to `.`) tests the working tree before you tag a release. It
checks `livery.json`, loads the base and every team, renders them for each harness, and scans every
file for secrets. It reads the working tree, untracked files included, not only what git tracks. It
reports every symbolic link and other non-regular file in `livery.json`, `base/` and `teams/`,
because `livery setup` refuses a shared repository that holds one. It reads only and exits 1 with
`N finding(s)` when a line says `finding`.

```console
$ livery check team-setup
ok       manifest: livery.json
ok       layer base: 7 items
ok       layer team:api: 1 items
ok       layer team:clash: 1 items
ok       layer team:web: 2 items
ok       render kiro: base and 3 teams render
ok       render claude: base and 3 teams render
ok       render opencode: base and 3 teams render
info     render opencode: instruction go applies to every file (opencode has no file patterns)
info     render opencode: instruction css applies to every file (opencode has no file patterns)
ok       secrets: 13 files scanned
```

- If `livery.json` is missing, `check` stops with one finding that names `livery doctor`; the
  other checks do not run. A malformed `livery.json` is a finding too, and the other checks still
  run.
- Notes from an adapter about the rendered setup show up as `info render <harness>: ...` lines;
  they do not make the check fail.
- The secret scan uses the rule set of gitleaks 8.30.1 and catches common token formats, not every
  secret. A repository's CI may run gitleaks itself as well. The secret itself is never printed.
- If the scan reports something that is not a secret, add one line `<path> <fingerprint>` to
  `secrets-allow` at the repository root (the path is relative to it and slash-separated; the
  fingerprint is the 12 characters the finding shows; `#` starts a comment line). A line that
  matches nothing is reported as `info`.
- A file that is not text (it holds a NUL byte) cannot be scanned. It is reported as a finding
  `<path>: binary file, not scanned`, so that unscanned content is never reported as clean. A
  binary file that is not a secret (an image, say) is accepted by listing it in `secrets-allow` as
  above; the fingerprint is derived from its path. `livery import` applies the same rule and names
  the line to add. There is no size limit: large text files are scanned in full.

## CI

In the shared repository, check every change:

```yaml
# CI step
- run: livery check .
```

In a project that installs from it (`--project`), check that the committed setup is intact:

```yaml
# CI step
- run: livery doctor --project
```

It exits 1 on a finding, for example a file livery owns that was edited by hand, or an installed
file or MCP entry that is missing (`livery update --project` restores it). Each job needs the livery
binary, git and read access to the shared repository.

## What you trust

The shared repository can install MCP servers, hooks and scripts, and the harness runs them as the
developer. Developers read `livery setup --source <address> --plan` before the first install and
`livery update --plan` whenever the shared repository changes hands. `--plan` shows the change; it does not
sandbox what the harness runs later.

In a project (`--project`) the shared repository writes into the repository's harness folders. The source is
chosen by whoever committed `.livery/config.json`, so in a repository you do not own, read
`livery update --project --plan` first.

livery refuses a source address with credentials in it (`https://token@host/...`,
`https://user:password@host/...`, `ssh://user:password@host/...`) and does not print it: such an
address would be saved in `config.json` and `lock.json` and, in a project, committed. `setup` saves
nothing and exits 2; `update` exits 2 and `doctor` reports a finding for a config that has one.
Use an SSH address (`git@host:team/setup.git`) or a git credential helper instead.
