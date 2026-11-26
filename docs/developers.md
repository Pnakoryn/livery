# For developers

Everything you need after the [quick start](../README.md#quick-start). In the output below, the
shared repository is `/home/you/team-setup`.

## Guided setup and flags

In a terminal, `livery setup` asks what it needs, shows the plan and asks before it writes
anything:

```console
$ livery setup
Set up: 1. this machine (your user account)  2. this project (/home/you/work; the files are committed for everyone)
Scope (1 or 2) [1]: 1
Address of your team's shared repository (git URL or folder): /home/you/team-setup
  1. kiro (detected)
  2. claude (detected)
  3. opencode
Coding agents to set up (numbers, comma-separated) [1,2]: 
  1. api
  2. clash
  3. web
Teams to add to the base setup (numbers, comma-separated, empty for none): 3
livery setup: /home/you/team-setup v0.2.0 (44a2de2c5ef0)
set     ~/.claude.json mcpServers.docs
...
write   ~/.kiro/steering/small.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
full diff: livery setup --source /home/you/team-setup --harness kiro --harness claude --team web --plan
Apply? [Y/n] y
saved   ~/.config/livery/config.json
done    15 change(s) applied
```

Answer `n` at `Apply?` and nothing is written (`nothing written`, exit 0). The scope question
("this machine" or "this project") appears only on a first setup inside a git repository; outside
one, `setup` installs for this machine and does not ask. A guided run prints the `full diff:` hint
with the command that shows the diff; without a question asked, `setup` prints the same list and
applies directly.

Without a terminal, for example in a script:

```sh
livery setup --source <address> --harness claude --team ""
```

Flags: `--source <address>`, `--harness <name>` (kiro, claude or opencode; repeat for several),
`--team <name>` (repeat for several), `--channel <channel>`, `--project`, `--plan`. Only `--source`
together with `--harness` and `--team` (or an existing config that holds them) asks nothing. With
no terminal, `setup` never asks. On the first setup `--source` is required (exit 2 without it); without `--harness` it installs for every harness folder it finds (exit 2 when there is none); without `--team` it installs no team. Give explicit choices, with `--team ""` for no team. A later `setup` keeps the saved source, harnesses and teams unless the flag is given, so `livery setup --channel next` needs no `--source`. `--plan` shows
what would change and writes nothing, except a copy of the shared repository in the cache; a diff
follows the list for files and entries that are written or changed, a removal shows only its list
line. `livery setup --help` has the rest.

## Updating, and going back to a release

```console
$ livery update
livery update: /home/you/team-setup v0.1.0 -> v0.2.0 (44a2de2c5ef0)
write   ~/.claude/rules/small.md
write   ~/.kiro/steering/small.md
to go back: livery setup --channel v0.1.0
done    2 change(s) applied
```

A run that changes something ends with a `done` line; `setup` also prints `saved` when it wrote the
configuration file. When nothing is new, `update` prints `up to date`. The executable bit of a
file follows the shared repository: when a release only changes it, `update` rewrites the file with the new
mode. A line `adopt` names a file, an entry or a list element that was already on disk with the
wanted content: livery writes nothing, and from then on owns it.

`livery update --plan` shows the same change, writes nothing and ends with a `plan` line:

```console
$ livery update --plan
livery update: /home/you/team-setup v0.1.0 -> v0.2.0 (44a2de2c5ef0)
write   ~/.claude/rules/small.md
write   ~/.kiro/steering/small.md
--- now/~/.claude/rules/small.md
+++ livery/~/.claude/rules/small.md
@@ -0,0 +1 @@
+Keep commits small.
...
plan    nothing was written; run the same command without --plan to apply
```

To try a release, update to it and look at the result. The `to go back` line is the command that
installs the old release again:

```console
$ livery setup --channel v0.1.0
livery setup: /home/you/team-setup v0.2.0 -> v0.1.0 (67c8740ddb74)
remove  ~/.claude/rules/small.md
remove  ~/.kiro/steering/small.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   ~/.config/livery/config.json
fixed on v0.1.0: livery update will not install newer releases; to follow releases again: livery setup --channel stable
done    2 change(s) applied
```

Your **channel** says which release you follow. Set it with `livery setup --channel <name>`:

| Channel | You get |
| --- | --- |
| `stable` (default) | the newest `vX.Y.Z` tag |
| `next` | the newest commit on the default branch |
| `vX.Y.Z` | exactly that release |

To look at another channel before you switch, run `livery setup --channel next --plan`; it
changes nothing.

### See what a shared repository offers

`livery list` prints the base items and every team of the shared repository at the configured
source, and marks the teams you chose with ` *`. `livery list --source <address> [--channel C]`
shows a shared repository without a config. Without `--installed` it shows what the shared
repository offers, not what is installed.

```console
$ livery list
/home/you/team-setup v0.2.0 (44a2de2c5ef0)
  base
    instruction  commits
    instruction  go
    instruction  small
    mcp          docs
    mcp          github
    kiro         hooks/lint.json
    skill        review
  team api
    skill        openapi
  team clash
    skill        openapi
  team web *
    instruction  css
    skill        review
```

`livery list --installed` shows what is installed: the source, tag and commit from the lock, every
item with its layer (`base`, `team <name>`, `personal`) and a state word when there is one
(`edited` by you, `kept, yours`, `missing`: deleted from disk, or `pending`: the next
`livery update` would change it), and under `yours` what is in your harness folders without livery
having installed it (the output ends with that `yours` block; it prints `(nothing)` when it is empty). It lists the release the lock records (its commit, teams and harnesses, not
the config) from the cache, without a fetch, so it works offline; your personal layer is read as
it is now. On channel `stable` it notes a newer release tag that the last fetch of any livery
command brought into the cache. Without that release in the cache (cache deleted) it exits 3: run
`livery update` first. It writes nothing, takes no `--source` or `--channel`, and exits 0 also for
`edited` items; `livery doctor` is the command that exits 1.

## Your own changes

- Files beside livery's are never touched. livery only replaces or removes files it recorded in
  its lock; everything else in `~/.kiro`, `~/.claude` and `~/.config/opencode` is yours.
- When a file livery wants to write already exists and livery did not put it there, livery leaves
  it and reports `kept, yours: <path> (the team's version is not installed; ...)`; the run still
  exits 0. `livery doctor` shows these. Rename or remove your file, then run `livery update`.
- Put your own skills, instructions and MCP servers in the personal layer,
  `~/.config/livery/personal/`. It has the same shape as `base/` in the shared repository (see
  [maintainers](maintainers.md#layout-of-the-shared-repository)) and is applied after the base and
  your teams.
- An MCP server livery installs is one entry of a shared file (`~/.kiro/settings/mcp.json`,
  `~/.claude.json`, `opencode.json` or `opencode.jsonc`). livery owns only the keys it writes in that entry
  (`command`, `args`, `env`, `url`, `headers`, and the harness's own forms of them). Keys the
  harness or you add to it, for example `disabled`, `autoApprove` or `timeout` in Kiro, are kept
  by every update and are not an edit. Changing one of livery's keys is an edit, as for a file.
  When the server leaves the release, the whole entry is removed, added keys included.
- livery reads every file again right before it writes it. A program that writes the same file in
  the same instant can still lose its change, so for a shared file such as `~/.claude.json` run
  `livery update` again if a harness setting seems to be missing.

### A file you edited

When you edit a file livery owns, `update` keeps your version, applies the rest of the release,
shows the diff of your file against the release, and exits 1. Every later run does the same until
you deal with the file. In this run `~/.kiro/steering/go.md` was edited by hand:

```console
$ livery doctor
...
finding  edited: ~/.kiro/steering/go.md -> delete it to take the team's version, or keep yours in the personal layer; livery update prints the steps
...
$ livery update
livery update: /home/you/team-setup v0.1.0 (67c8740ddb74)
--- now/~/.kiro/steering/go.md
+++ livery/~/.kiro/steering/go.md
@@ -4,4 +4,3 @@ fileMatchPattern: ["**/*.go"]
 ---
 
 Use the standard library first. Wrap errors with context.
-# mine
edited  ~/.kiro/steering/go.md: you changed it; livery kept your version and did not install the team's
        to take the team's version: delete ~/.kiro/steering/go.md, then run livery update
        to keep your change: put your text into ~/.config/livery/personal/instructions/go.md (Markdown; the only frontmatter keys are description and globs), delete ~/.kiro/steering/go.md, then run livery update
livery: 1 edited by you, kept; the rest is applied
```

`livery update --plan` lists the file as `edited by you: ~/.kiro/steering/go.md`. When the item
leaves the release while you have it edited, `update` exits 0, leaves the file active and yours,
and prints `left, edited by you: <path> (no longer in the release; the file is yours now, delete it to remove it)`:
nobody maintains it any more. There is no `--force`. Two ways out:

1. **Take the team's version:** delete the file (for an MCP server: remove its entry), then run
   `livery update`.
2. **Keep yours:** write it to the personal layer, at the path the `edited` block names, in the
   format of the shared repository, not in the harness's format. Remove the installed file,
   then run `livery update`: it installs your version from the personal layer.

For an instruction, the format of the shared repository is the text with only `description` and `globs` in the
frontmatter. Say you changed the Kiro steering file `~/.kiro/steering/commits.md`:

```markdown
---
inclusion: always
---

Write commit subjects in the imperative mood, at most 72 characters.
Mention the ticket number.
```

Your version in `~/.config/livery/personal/instructions/commits.md` has no `inclusion:` line (an
always-on instruction needs no frontmatter; for a file pattern, Kiro's `fileMatchPattern` becomes
`globs`):

```markdown
Write commit subjects in the imperative mood, at most 72 characters.
Mention the ticket number.
```

When the edited file already comes from your personal layer, change it there instead. In a
project (`--project`) there is no personal layer: delete the file and run
`livery update --project`, or change it in the shared repository.

### Symbolic links

Symbolic links are never followed. A linked file is `kept, yours`. A linked folder below the
harness folder (`~/.kiro/steering`, `~/.claude/rules`, `~/.config/opencode/skills`, ...) is
treated like a linked file: livery writes nothing into it and never removes the link. The harness
folder itself (`~/.kiro`, `~/.claude`, `~/.config/opencode`) may be a link, for example one
managed by your dotfiles. A linked `mcp.json`, `.claude.json`, `opencode.json` or `opencode.jsonc`, or a linked
folder holding one, stops the run with exit 1: replace the link with a regular folder or file,
then run `livery update`. After you move a harness folder (`KIRO_HOME`, `CLAUDE_CONFIG_DIR`,
`XDG_CONFIG_HOME`), livery removes its files from the old place only when no folder on the way
below your home folder is a link; otherwise they are `left, edited by you`.

## Variables of MCP servers

A shared MCP server never holds a secret; it names variables as `${VAR}` and each person sets the
variable in their own shell or environment, before starting the coding agent. When an installed
server needs a variable that is empty in the current shell, livery prints:

```text
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
```

- `setup` prints the `note` on every run, for the variables of an installed server (`NAME, OTHER
  are not set ...; set them ...` for several).
- `update` prints it only when the run sets that server's entry.
- `doctor` prints the same as an `info` line, `info     mcp github: ... -> set it before ...`.
- `${VAR:-default}` is not listed.

Set the variable where your coding agent starts, for example `export GITHUB_TOKEN=...` in your
shell's startup file. The note is no finding: the exit code stays 0.

## Machine and project

livery works in two scopes. They are independent: neither reads the other's configuration or
lock, and the same item in both is installed twice.

| | Machine (default) | Project (`--project`) |
| --- | --- | --- |
| For | your own setup, on your machine | the setup a repository needs, for everyone who clones it |
| Files go to | your home directory (`~/.kiro`, `~/.claude`, ...) | the project root |
| Config and lock | `~/.config/livery/` | `.livery/config.json` and `.livery/lock.json` in the project |
| Personal layer | yes | no |

`--project` works on `setup`, `update`, `uninstall`, `doctor`, `list` and `import`; without it
every command works on the machine. The project root is the nearest folder, from the working
directory upward, that holds `.livery/config.json`; `setup --project` without one uses the working
directory. A project install never writes outside the project root, also not through a link. The
source cache stays in `~/.cache/livery/`. The paths per harness are in
[reference.md](reference.md#paths-per-harness).

```console
$ cd proj
$ livery setup --project --harness claude --source /home/you/team-setup
project: /home/you/proj
livery setup: /home/you/team-setup v0.2.0 (44a2de2c5ef0)
write   .claude/rules/commits.md
write   .claude/rules/go.md
write   .claude/rules/small.md
write   .claude/skills/review/SKILL.md
write   .claude/skills/review/scripts/check.sh
set     .mcp.json mcpServers.docs
set     .mcp.json mcpServers.github
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   .livery/config.json
commit .livery/ and the files above so your team gets the same setup
done    7 change(s) applied
$ livery doctor --project
project: /home/you/proj
ok       git: /usr/bin/git
ok       config: /home/you/proj/.livery/config.json
ok       lock: /home/you/proj/.livery/lock.json
ok       source: /home/you/team-setup (channel stable)
ok       installed: v0.2.0 (44a2de2c5ef0)
ok       edited: no installed file was changed by hand
ok       kept, yours: nothing of yours is in the way
info     mcp github: GITHUB_TOKEN is not set in this shell -> set it before you start your coding agent
ok       update: up to date
```

- Commit `.livery/` (config and lock) and the files livery writes into the project: then everyone
  who clones the repository gets the same setup, and `livery doctor --project` can check it. In
  CI, `- run: livery doctor --project` exits 1 on a finding, for example a file livery owns that
  was edited by hand, or an installed file or MCP entry that is missing (`livery update --project`
  restores it). The job needs the livery binary, git and read access to the shared repository.
- In a project that has no harness folder yet, name the harness with `--harness`.
- A local path as `--source` is stored as an absolute path and only works on machines where that
  path exists; use a git URL for a project your team clones.
- A lost or damaged `.livery/lock.json` is best restored from git; without it
  `livery update --project` adopts the files and entries that already have the wanted content.
  The lock carries a `schema` number; a lock written by a newer livery asks you to upgrade livery.
- A cloned repository may contain symbolic links, so in a project no link is followed, not even
  the harness folder (`.kiro`, `.claude`, `.opencode`): a file below a link is `kept, yours`, and
  a shared JSON file below one stops the run with exit 1. When `.livery`, `.livery/config.json` or
  `.livery/lock.json` is a link, every `--project` command stops with exit 1: replace the link
  with a real folder or file. A project lock that names a file below `.git/` or `.livery/` is
  refused.
- The source is chosen by whoever committed `.livery/config.json`: in a repository you do not own,
  read `livery update --project --plan` first.

## Teams

A shared repository can hold a `teams/<team>/` folder per team, with the same shape as `base/`.
Install a team's content with `livery setup --team <name>`.

`--team` and `--harness` replace the saved list, they do not add to it. To add a team, repeat the
ones you have: `livery setup --team web --team platform`. `--team ""` installs no team.

Layers apply in this order: `base`, your teams, your personal layer. If two layers
define an item of the same kind and name, the later layer wins (`livery doctor` reports it as an override). If two
teams you chose define the same item, that is a clash: livery stops with exit 1 and names both
files. An item with that name in your personal layer resolves the clash: the personal item wins and
livery does not stop. `livery list` shows which teams exist.

## Undo

`uninstall` removes what the lock names and leaves the rest: files you edited, files you added,
your personal layer, and the configuration file.

```console
$ livery uninstall --plan
livery uninstall
unset   ~/.claude.json mcpServers.docs
...
left, edited by you: ~/.kiro/steering/go.md (you changed it, so it stays; delete it to remove it)
plan    nothing was written; run the same command without --plan to apply
$ livery uninstall
livery uninstall
unset   ~/.claude.json mcpServers.docs
...
left, edited by you: ~/.kiro/steering/go.md (you changed it, so it stays; delete it to remove it)
kept    ~/.config/livery/config.json
done    12 removed; to install again: livery update; to remove livery's own files too: delete ~/.config/livery
```

Run `livery update` to install again; the `kept` lines name the configuration file and the personal
folder.
