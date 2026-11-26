# Reference

## Words

One name per thing, in the docs, the help and the messages.

- **shared repository**: the git repository that holds the setup. No other name.
- **source**: the address of the shared repository: the `--source` flag and the `source` key of the
  config. Nothing else.
- **release**: a tag `vX.Y.Z` of the shared repository. **channel**: `stable` (newest release),
  `next` (newest commit) or one release tag.
- **harness**: the coding-agent program (Kiro, Claude Code, opencode).
- **layer**: `base`, one team, or your personal folder. **team**: an optional named layer in
  `teams/<name>/`.
- **item**: one skill, instruction, MCP server, or harness-only file. Two items are the same when
  kind and name match (and the harness, for a harness-only file).
- **entry**: one key livery owns in a JSON file the harness also writes.
- **lock**: the record of the installed release and of every file and entry livery owns.

## Commands

`livery --help` lists the commands and `livery <command> --help` the flags and examples of one.

| Command | What it does |
| --- | --- |
| `setup` | create or change the config, then install |
| `update` | install or update the shared setup from the shared repository at the configured source |
| `uninstall` | remove what livery installed and you did not change; keep the config |
| `import` | write a harness folder into a shared repository folder; never commits |
| `list` | show the teams and items a shared repository offers, or with `--installed` what is installed |
| `check` | validate a shared repository folder and scan it for secrets |
| `doctor` | one line per check, exit 1 on a finding |
| `version` | print the version |

Most commands take `--plan` (show the change, write nothing) and `--project` (work on the project
around the working directory, not the machine). Other flags: `setup` takes `--source`,
`--channel`, `--harness`, `--team`; `list` takes `--source`, `--channel`, `--installed`; `import`
takes `--from`, `--into`, `--team`, `--only`. See [developers.md](developers.md) for developers'
use and [maintainers.md](maintainers.md) for `check`.

`livery import` reads a harness folder on this machine (or, with `--project`, in the project) and
writes its skills, instructions, MCP servers and harness-only files (Kiro's folders, Claude Code
subagents) as new files into a shared repository folder. It
never commits, and a skills folder that cannot be read is reported as a `skip` line
while the import continues. `--from` can be repeated only with `--project`. The steps are in
[onboarding.md](onboarding.md).

## Files livery writes

| What | Where |
| --- | --- |
| Configuration | `~/.config/livery/config.json` |
| Lock (the installed release, the files and the JSON entries livery owns) | `~/.config/livery/lock.json` |
| Personal layer | `~/.config/livery/personal/` |
| Project configuration and lock | `<project>/.livery/config.json`, `<project>/.livery/lock.json` (commit them) |
| Cache | `~/.cache/livery/` |

`XDG_CONFIG_HOME` and `XDG_CACHE_HOME` move them when set to an absolute path. Deleting the cache
is safe: the next run that fetches (`setup`, `update`, `list`, `doctor`) fetches the shared
repository again. `livery list --installed` never fetches: it exits 3 when the installed release is
not in the cache; run `livery update` first.

## Paths per harness

On the machine:

| | Kiro | Claude Code | opencode |
| --- | --- | --- | --- |
| Skills | `~/.kiro/skills/<name>/` | `~/.claude/skills/<name>/` | `~/.config/opencode/skills/<name>/` |
| Instructions | `~/.kiro/steering/<name>.md` | `~/.claude/rules/<name>.md` | `~/.config/opencode/livery/instructions/<name>.md`, plus one element of `instructions` in the opencode config (`opencode.json`, or `opencode.jsonc` when that file exists) that points at that folder |
| MCP servers | `mcpServers` in `~/.kiro/settings/mcp.json` | `mcpServers` in `~/.claude.json` | `mcp.servers` in `~/.config/opencode/opencode.json`, or in `opencode.jsonc` there when that file exists |
| Harness-only files | `kiro/<dir>/<path>` to `~/.kiro/<dir>/<path>` (`<dir>`: `steering`, `agents`, `hooks`, `workflows`) | Claude Code subagents: `claude/agents/<path>` to `~/.claude/agents/<path>` | not supported |
| Moves the folder | `KIRO_HOME` | `CLAUDE_CONFIG_DIR` | `XDG_CONFIG_HOME` |

In a project (`<project>` is the project root):

| | Kiro | Claude Code | opencode |
| --- | --- | --- | --- |
| Skills | `<project>/.kiro/skills/<name>/` | `<project>/.claude/skills/<name>/` | `<project>/.opencode/skills/<name>/` |
| Instructions | `<project>/.kiro/steering/<name>.md` | `<project>/.claude/rules/<name>.md` | `<project>/.opencode/livery/instructions/<name>.md`, plus the element `.opencode/livery/instructions/*.md` in `instructions` |
| MCP servers | `mcpServers` in `<project>/.kiro/settings/mcp.json` | `mcpServers` in `<project>/.mcp.json` | `mcp.servers` in the project's opencode config |
| Harness-only files | `kiro/<dir>/<path>` to `<project>/.kiro/<dir>/<path>` | Claude Code subagents: `claude/agents/<path>` to `<project>/.claude/agents/<path>` | not supported |

In a project `KIRO_HOME`, `CLAUDE_CONFIG_DIR` and `XDG_CONFIG_HOME` are ignored. The opencode
config is the first that exists of `<project>/opencode.jsonc`, `<project>/opencode.json`,
`<project>/.opencode/opencode.jsonc`, `<project>/.opencode/opencode.json`; without one, livery
creates `<project>/opencode.json`.

On a machine, livery uses an existing `opencode.jsonc` in the opencode folder; without one it
uses `opencode.json`.

With `CLAUDE_CONFIG_DIR` set, the MCP servers go to `.claude.json` inside that folder. A relative
value is taken from your home directory. livery changes only the entries it owns inside the JSON
files. In a file indented with two spaces, the layout the harnesses write themselves, every byte
outside the entries livery owns stays as it was. A file in another layout (tabs, four spaces, one
line) is re-indented to two spaces on the first change; its values stay equal.

Symbolic links: a link is never followed, except that on a machine the harness folder itself
(`~/.kiro`, `~/.claude`, `~/.config/opencode`) may be a link. In a project no link at all is
followed. A linked file is `kept, yours`; see [developers.md](developers.md#symbolic-links).

Limits of the opencode adapter:

- livery edits strict JSON only. An `opencode.jsonc` with comments or trailing commas stops the run
  with exit 1 ("not valid JSON"); remove the comments or keep the file valid JSON.
- opencode has no file patterns for instructions: an instruction with `globs` applies to every file
  there, and `setup` and `update` print a `note` line for each.
- opencode has no default values for variables: `${NAME}` becomes `{env:NAME}`, `${NAME:-x}` is an
  error.
- Native files for opencode are not supported yet.
- Needs opencode 2.3.2 or newer: that is the version the `mcp.servers` shape was checked with; older
  versions are not tested. opencode 2.3.2 reads servers under `mcp.servers` and normalises servers
  written directly under `mcp` to the same place.
- livery always writes servers under `mcp.servers`. `livery import` reads both places.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | ok |
| 1 | a finding: a failed `doctor` check, a hand-edited file, a clash, a shared repository without a release |
| 2 | bad input: a flag, the config, or the content of the shared repository; a release tag that does not exist; an invalid channel name; the message names it |
| 3 | something missing in the environment; the message names it and the fix |

## What livery does not do

- No Kiro powers.
- No `try` command to run a harness against another release.
- No `diff` command; `--plan` shows the diff.
- No `--force`.
- macOS binaries are built but not yet verified on a Mac.
- No Windows binary, and so no project scope on Windows. Use WSL. A Kiro IDE that runs on Windows
  itself reads the Windows-side folder, which livery inside WSL does not write. This is not
  verified on a Windows machine.

## Words in the output

Almost every line of `setup`, `update`, `uninstall`, `import`, `list`, `doctor` and `check` starts
with a word of the table below. A second table lists other lines that need explaining; the first
line of a run (`livery setup: ...`), `project:`, `next steps:` and the sentences after them say what
they mean themselves. `livery <command> --plan` prints the same lines and writes nothing.

| Word | Meaning |
| --- | --- |
| `write` | a file that is created or changed |
| `remove` | a file livery owns that is deleted |
| `set` | an entry (or a list element) that is added or changed in a JSON file the harness also writes |
| `unset` | an entry that is removed from such a file |
| `adopt` | the file or entry is already on disk with the wanted content: nothing is written, and livery owns it from now on |
| `kept, yours` | a file or entry livery would write exists and is not in the lock: livery leaves it; rename or remove yours, then run `update` |
| `edited by you` | (in `--plan` and `list`) a file or entry livery owns that you changed |
| `left, edited by you` | (`uninstall`) an owned file you changed is left in place; (`setup`, `update`) the file or entry you changed is no longer in the release, so it stays and is yours now. Delete it to remove it |
| `edited` | (after a run) the same, with the steps to take the team's version or keep yours; the run exits 1. `doctor` and `list --installed` use the word as a check or state |
| `note` | something to know, no finding: a variable that is not set, a newer release, an instruction opencode cannot limit to files, a partial import |
| `saved` | the config file was written |
| `config` | (`setup --plan`) the config that would be saved |
| `done` | the run is over, with the number of changes applied |
| `plan` | (`--plan`) nothing was written; run the same command without `--plan` |
| `kept` | (`uninstall`) the config or the personal layer stays |
| `mode` | only the executable bit of a file changes |
| `skip` | (`import`) something not taken, with the reason on the line |
| `same` | (`import`) the file exists with the same content, or the item is identical in several harnesses and imported once |
| `var` | (`import`) a variable each developer sets before starting the harness |
| `ok` | (`doctor`, `check`) the check passed |
| `info` | (`doctor`, `check`) something to know; the exit code stays 0 |
| `finding` | (`doctor`, `check`, `import`) the check failed; exit 1. Most lines name the fix after `->`, some name it in the message itself |

Lines that do not start with a word of the table:

| Line starts with | Meaning |
| --- | --- |
| `warning:` | something to act on that is not a finding, for example a release tag that moved since the last run |
| `to go back:` | (`update`) the command that returns to the release you had before |
| `fixed on <release>:` | (`update`, `setup`) your channel is one release, so `update` installs nothing newer; the line names the command that follows releases again |
| `left out:` | (`import`) how many items were not imported because livery installed them |
| `full diff:` | (`setup` in a terminal) the command that prints the full diff, asked before `Apply?` |

`up to date` is printed alone when nothing changes. `list --installed` adds a state after the
layer of an item: `edited`, `kept, yours`, `missing` (deleted from disk) or `pending` (the next
`update` would change it).
