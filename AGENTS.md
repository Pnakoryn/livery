# livery

livery is a Go command-line tool that installs and updates a team's shared coding-agent setup
(skills, instructions, MCP servers) on a developer's machine or, with `--project`, in a
repository (committed, so every clone has it). It takes the setup from tagged
releases of a shared git repository and puts team and personal layers on top. The core knows no
harness; one adapter per harness (Kiro, Claude Code and opencode today) says where files go.

## Commands
- `make build`: static binary `./livery`.
- `make test`: `go test ./...`.
- `make e2e`: user journeys in `e2e/` (build tag `e2e`): the built binary in a throwaway home.
- `make lint`: `golangci-lint run ./...` (config in `.golangci.yml`).
- `make check`: all of the above and a vet of the macOS build; run it before a pull request.
- `make lint-ci`: `zizmor` on `.github/workflows/*.yml`.
- Tool versions are pinned in `mise.toml`; `mise install` sets them up.

## Rules that apply to every change
- Standard library only. A new module in `go.mod` needs a clear reason, stated in the pull request.
- git is the only subprocess. No git library, no host API (any git host works because only git
  is used), no harness CLI.
- Exit codes (`internal/exit`): 0 ok, 1 a finding (doctor check, drift, collision), 2 bad input (a flag,
  the config, or the content of the source), 3 something missing in the environment. Every error message names the file or flag and the next
  command to run.
- No terminal (stdin or stdout not a TTY): never prompt. A first `setup` then needs only `--source`
  (exit 2 naming the flag without it), installs for every harness folder it finds (exit 2 when
  there is none) and for no team; a later `setup` keeps the saved choices unless a flag changes
  them. A choice that cannot be made that way exits 2 naming the flag.
- Writes apply by default; `--plan` shows the change and writes nothing outside the source cache.
- livery only replaces or removes files recorded in its lock. A file it does not own is never
  overwritten: it is reported as `kept, yours`.
- The core (`internal/` except `internal/adapter/<name>`) knows no harness: no `.kiro`, `.claude`
  or other harness path, file name or format outside an adapter package.
- Adapters are registered in one place, `internal/engine` (`Adapters`); it is the only core
  package that imports an adapter package.
- A hand-edited file or entry livery owns is skipped: the rest of the run is applied, the diff is
  shown, and the run exits 1 until the user removes or reverts it, or keeps their version in the
  personal layer. There is no `--force`.
- A symbolic link is never followed: livery does not change files in places it does not know.
  A linked file is `kept, yours`; a linked shared JSON file stops the run with exit 1. One
  exception, on a machine only: the harness folder itself may be a link (dotfiles).
- Project scope (`--project`): an install never writes outside the project root, also not through
  a link; lock paths are relative to the project root; there is no personal layer; the harness
  environment variables (`KIRO_HOME`, `CLAUDE_CONFIG_DIR`, ...) are ignored.
- Linux and macOS. Paths come from the home directory and `XDG_*` with `~/.config`, `~/.cache`
  fallbacks on both; never `os.UserConfigDir` (it differs on macOS).
- No telemetry, no network except git fetches of the configured source.

## Code conventions
- Tests that run git set `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, the
  `GIT_AUTHOR_*` and `GIT_COMMITTER_*` variables, and use `git init -b main`: they must pass
  with an empty, read-only home directory.
- Tests write only below `t.TempDir()`.
- A change adds the tests it needs, as new functions or new files, and may change existing tests
  when the behaviour they pin changes on purpose.
