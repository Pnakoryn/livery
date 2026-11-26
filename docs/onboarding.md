# Start a shared repository

For the person who sets up the team's shared repository from a setup that already works on one
machine. Developers who only install read the [README](../README.md) and [developers.md](developers.md); the layout and checks of the repository are in [maintainers.md](maintainers.md).

## What you need

- `git` and `livery` on your `PATH` (`livery version` prints the version).
- One machine with the setup you want to share, in the Kiro folder (`~/.kiro`), the
  Claude Code folder (`~/.claude`) or the opencode folder (`~/.config/opencode`).
- An empty folder for the shared repository; livery creates the files in it.

To see a finished shared repository first, or to copy one as a starting point, use
[livery-example](https://github.com/Pnakoryn/livery-example) (tags `v0.1.0` and `v0.2.0`, a
`base/` layer and the team `web`, for Claude Code and Kiro).

## Import

`--plan` shows what would be written and writes nothing:

```sh
livery import --from kiro --into ~/team-setup --plan
livery import --from kiro --into ~/team-setup
```

`--from` is `kiro`, `claude` or `opencode`. In a terminal, `import` lists what it found and asks
`Import which (numbers, comma-separated, empty for all):`; Enter takes all. `import` only reads the harness folder, never commits, and
writes only new files: if a file exists with other content it stops with exit 1 and writes
nothing.

Each output line starts with a word; [Words in the output](reference.md#words-in-the-output) lists
them. The ones to read first: `write` (a file that will be created), `same` (the file exists with
the same content) and `skip` (something livery did not take, with the reason on the same line: a
symbolic link, a name that breaks the rule, a skill without `SKILL.md`, a disabled MCP server,
settings livery does not support yet). Fix what the line says and import again, or leave it out
on purpose.

Two name rules apply. An MCP server name matches `^[A-Za-z0-9][A-Za-z0-9._-]*$` (so
`awslabs.core-mcp-server` and `My_Server` import under their own names). Skills, instructions and
teams match `^[a-z0-9][a-z0-9-]*$`.

MCP servers keep their shape, but every `env` value is replaced by `${NAME}` (the name of the
key). The value of a header named `Authorization`, `Proxy-Authorization` or `Cookie`, or whose
name contains `key`, `token`, `secret` or `password` (any case), is replaced by
`${<SERVER>_<HEADER>}` in upper case with every other character turned into `_` (server
`github`, header `Authorization`: `${GITHUB_AUTHORIZATION}`); a leading `Bearer ` or `Basic `
is kept, and a value that already holds a `${NAME}` reference is left as it is. Everything else
(other headers, `args`, `command`, `url`) is written as read and checked by the secrets scan: a
possible secret stops the import with a finding. A `url` with a password in it
(`https://user:pass@host`) also stops the import; move the password into a header or a
variable. Import prints one `var` line per variable (`var     NAME: server S, place (how)`;
the how says whether the value was replaced by `${NAME}` or already was a reference) and then
the line `everyone who installs this needs these variables set in the shell that starts the coding
agent`: each developer sets them in their own environment. A `note` line is a `skip` that is only
partial: part of the item was imported and the line says what was left out. To put a harmless
value back (say `LOG_LEVEL=debug`), edit `base/mcp/<server>.json` and write the value in place
of `${LOG_LEVEL}`. If `livery check` then reports it as a possible secret, add the line it
prints to `secrets-allow` (see Check).

To import only some items, repeat `--only KIND/NAME`, for example `--only skill/review`.

## Review the folder

| Path | Content |
| --- | --- |
| `livery.json` | Manifest, written once |
| `base/` | What every developer gets |
| `teams/<team>/` | What only developers of that team get; same layout as `base/` |
| `skills/<name>/` | A skill: `SKILL.md` and its files, executable bits kept |
| `instructions/<name>.md` | An instruction; `globs` in the frontmatter limit it to matching files |
| `mcp/<server>.json` | One MCP server |
| `kiro/` | Files that only Kiro knows, in Kiro's own layout (for example `kiro/hooks/`) |
| `claude/` | Files that only Claude Code knows: subagents in `claude/agents/` |

The `skills/`, `instructions/`, `mcp/` and harness folders (`kiro/`, `claude/`) are inside `base/` or `teams/<team>/`; `teams/` is at the root. Read the files: this is
what your team will run.

## Check

```sh
livery check ~/team-setup
```

`check` validates the folder and scans it for secrets. Exit 0 means it is clean; exit 1 means
findings, each with the next step. A value that looks like a secret but is not goes into the
file `secrets-allow` in the folder, one line `<path> <fingerprint>` as the finding prints it.

## Publish

```sh
git -C ~/team-setup init -b main
git -C ~/team-setup add -A
git -C ~/team-setup commit -m "first release"
git -C ~/team-setup tag v0.1.0
git -C ~/team-setup remote add origin <address>
git -C ~/team-setup push origin main v0.1.0
```

The channel `stable` follows the highest tag `vX.Y.Z`. Publish a change by committing and
pushing a new, higher tag.

## Switch your own machine

After the import your own files are still in the harness folder. livery only replaces files it
recorded in its lock, so run setup on this machine against the shared repository:

```sh
livery setup --source <address>
```

The output of a run (`--source` is a local folder here; the files are the ones imported above):

```text
livery setup: /home/you/team-setup v0.1.0 (bba907ff99b2)
kept, yours: ~/.claude.json mcpServers.github (the team's version is not installed; to install it, rename or delete yours, then run livery update)
adopt   ~/.claude/skills/deploy/SKILL.md
adopt   ~/.claude/skills/review/SKILL.md
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   ~/.config/livery/config.json
done    2 change(s) applied
```

- `adopt`: the file on disk is identical to the shared one. Nothing is written; livery owns it
  from now on.
- `kept, yours`: import changed the item (a secret replaced by `${NAME}`, frontmatter rewritten),
  so your file differs and is never overwritten.

For each `kept, yours` line: rename or delete your file (`mv <file> <file>.old`), or for an entry
in a shared JSON file remove the entry, then run `livery update`. Here the line is for the entry
`mcpServers.github` in `~/.claude.json`: delete that entry from the file (the `github` key under
`mcpServers`), then:

```sh
livery update
livery doctor
```

```text
livery update: /home/you/team-setup v0.1.0 (bba907ff99b2)
set     ~/.claude.json mcpServers.github
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
done    1 change(s) applied
```

`doctor` ends the switch with `ok       kept, yours: nothing of yours is in the way`. Set the
variables of the `var` lines in the shell that starts your coding agent. To keep something of
your own on purpose, put it in the personal layer.

## Tell the team

Copy this message and fill in the angle brackets:

```text
Hi all,

our coding-agent setup (skills, instructions, MCP servers) now comes from one shared
repository. To install it on your machine:

1. Get livery: download the binary from livery's release page: <address of livery's release
   page> (not the address of the shared repository)
2. Run: livery setup --source <address of the shared repository>
   (teams: add --team <name>, for example --team web; ask <person> which one you need)
3. Set these variables in the shell that starts your coding agent:
   <NAME>: <where to get the value, for example the team password manager entry "...">
   (the variables are the `var` lines of the import)
4. Check the result with: livery doctor

From now on run `livery update` now and then (for example on Monday): it brings the newest
release. If something looks wrong, answer here.
```

In a terminal `livery setup` asks for the scope, the harnesses and the teams, shows the plan (the
hint `full diff: livery setup --source ... --harness ... --team ... --plan` prints the command for
the full diff) and asks `Apply? [Y/n]`. Without a terminal it never asks: on the first setup `--source` is required (exit 2 without
it; a later setup keeps the saved choices), without `--harness` it installs for every harness folder it finds, and without `--team` no
team. For scripts give every choice, with `--team ""` for no team:

```sh
livery setup --source <address> --harness claude --team web
```

## Change something later

Edit the file in the shared repository (for example `base/skills/review/SKILL.md`), then:

```sh
livery check ~/team-setup
git -C ~/team-setup commit -am "review skill: ask for tests"
git -C ~/team-setup push origin main
```

Try the change before a release on one machine: `next` is the newest commit, `stable` the newest
tag.

```sh
livery setup --channel next
```

The first line names the channel and the commit:

```text
livery setup: /home/you/team-setup next (a72bf9fa34b7)
```

To return to the newest tag and undo the change on your machine:

```sh
livery setup --channel stable
```

When you are satisfied, tag the next release
(`git tag v0.1.1`, `git push origin v0.1.1`, see Release rules); only then does `stable` include
the change. Developers get it with `livery update`.

## Add a team later

Write only the team's items, with `--only` (repeat it for several):

```sh
livery import --from claude --into ~/team-setup --team web --only instruction/design
livery check ~/team-setup
```

```text
left out: 3 items that livery installed
livery import: wrote into /home/you/team-setup (from /home/you/.claude)
write   /home/you/team-setup/teams/web/instructions/design.md
next steps:
  livery check /home/you/team-setup
  review and commit the folder (git -C /home/you/team-setup init first if it is not a repository)
  tag a release: git -C /home/you/team-setup tag vX.Y.Z (and push it)
  livery setup --source /home/you/team-setup
  on this machine your own files stay as they are (kept, yours); docs/onboarding.md says how to switch
```

Without `--only` the whole harness folder is written under the team a second time (items livery
installed on this machine are left out, items you own are not). Commit and tag a release: a team
exists for developers only from a tag on (`stable`). They run `livery setup --team web`.

An item with the same kind and name in two teams is a clash for a developer who has both teams.
`check` does not report it, `setup` does:

```text
livery: team:api: instructions/design.md and team:web: instructions/design.md: the same item in two teams; ask the owners to remove it from one, or put your own version into the personal layer
```

Keep such an item in `base/`.

## Release rules

- A release is a tag `vX.Y.Z`; `stable` installs the highest one.
- Never move a tag. livery warns every developer who had it:
  `warning: tag v0.2.0 moved from 6d3682bb5d3c to 3e3d73e0b353 since the last run; a release tag should never change, ask the owner of the shared repository`.
  Publish a change as a new, higher tag.
- A developer goes back with `livery setup --channel <tag>` (for example `v0.1.0`).
- Delete no tag a developer may be fixed on.

## A project

One person runs this in the repository and commits `.livery/` and the files livery wrote;
everyone else just pulls (a project install has no personal layer):

```sh
livery setup --project --source <address the team can clone> --harness claude --team web
git add .livery .claude .mcp.json
git commit -m "livery setup"
```

```text
project: /home/you/work
livery setup: /home/you/team-setup v0.1.2 (0f5300999ee2)
write   .claude/rules/design.md
write   .claude/skills/deploy/SKILL.md
write   .claude/skills/review/SKILL.md
set     .mcp.json mcpServers.github
note    mcp github: GITHUB_TOKEN is not set in this shell; set it before you start your coding agent
saved   .livery/config.json
commit .livery/ and the files above so your team gets the same setup
done    4 change(s) applied
```

To update, one person runs `livery update --project`, reviews the diff and commits. CI checks
drift with `livery doctor --project` (see CI).

## CI

Both snippets are for a GitHub Actions workflow (`.github/workflows/livery.yml`; other CI systems
need the same steps). The install step downloads a pinned release of livery from
`https://github.com/Pnakoryn/livery/releases` and checks it against `SHA256SUMS`. Replace `0.1.0`
by the version you use, and the platform if your runner is not Linux on amd64.

A private shared repository: give CI an SSH deploy key (read access, stored as the secret
`LIVERY_DEPLOY_KEY`) or let the CI's git credential helper answer. livery only runs git and never
asks for a password.

`livery check` in the shared repository:

```yaml
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install livery
        run: |
          v=0.1.0
          f=livery-$v-linux-amd64
          base=https://github.com/Pnakoryn/livery/releases/download/v$v
          curl -fsSLO $base/$f
          curl -fsSLO $base/SHA256SUMS
          sha256sum -c --ignore-missing SHA256SUMS
          sudo install -m 755 $f /usr/local/bin/livery
      - run: livery check
```

`livery doctor --project` in a project (the shared repository is fetched from the address in
`.livery/config.json`, so a private one needs the deploy key):

```yaml
on: [push, pull_request]
jobs:
  doctor:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install livery
        run: |
          v=0.1.0
          f=livery-$v-linux-amd64
          base=https://github.com/Pnakoryn/livery/releases/download/v$v
          curl -fsSLO $base/$f
          curl -fsSLO $base/SHA256SUMS
          sha256sum -c --ignore-missing SHA256SUMS
          sudo install -m 755 $f /usr/local/bin/livery
      - name: Deploy key for a private shared repository
        run: |
          mkdir -p ~/.ssh
          printf '%s\n' "${{ secrets.LIVERY_DEPLOY_KEY }}" > ~/.ssh/id_ed25519
          chmod 600 ~/.ssh/id_ed25519
          ssh-keyscan <host of the shared repository> >> ~/.ssh/known_hosts
      - run: livery doctor --project
```

## From an existing repository

A repository that already holds agent setup (`.kiro/`, `.claude/`, `.mcp.json`, ...) can become a
shared repository. Run import from the repository with `--project`; it reads the harness folders of the
project instead of your machine's:

```sh
livery import --project --from claude --into ~/team-setup
```

Repeat `--from` to read several harnesses in one run. Items that the project's own lock
installed are left out. Project notes such as `CLAUDE.md` or `AGENTS.md` are reported as `skip`:
livery does not manage them.

### The full path

1. Import from the repository (`--plan` first to see the list):

   ```sh
   cd ~/work/first-project
   livery import --project --from claude --into ~/team-setup
   ```

2. Check the folder:

   ```sh
   livery check ~/team-setup
   ```

3. Commit and tag a release (and push it, as in Publish):

   ```sh
   git -C ~/team-setup init -b main
   git -C ~/team-setup add -A
   git -C ~/team-setup commit -m "first release"
   git -C ~/team-setup tag v0.1.0
   ```

4. In a second repository, install it. Name the harness when the repository has no harness
   folder yet:

   ```sh
   cd ~/work/second-project
   livery setup --project --harness claude --source git@example.com:team/setup.git --plan
   livery setup --project --harness claude --source git@example.com:team/setup.git
   livery doctor --project
   ```

   A local path as `--source` is stored as an absolute path and only works on machines where that path exists.

5. Commit `.livery/` and the files livery wrote, so the team gets them with a clone. Later
   releases arrive with `livery update --project`.
