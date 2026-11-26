# Contributing to livery

You need Go and make; nothing else is required to build, test and send a change. The rules for
code are in [AGENTS.md](AGENTS.md); read them first.

## Before you start

A larger change (a new adapter, a new command, a change of the lock or config format) starts with
an [issue](https://github.com/Pnakoryn/livery/issues), so that the direction is agreed before you
write code. Small fixes go straight to a pull request.

## Prerequisites

- Go 1.27 or newer (the `go` line in `go.mod`).
- `git` and `make`.
- `golangci-lint` 2.15.0 for `make lint`.
- Optional: `mise install` installs the pinned versions of the tools from `mise.toml`.

## Clone, build, test

```sh
git clone https://github.com/Pnakoryn/livery.git
cd livery
make build      # writes ./livery, with the version from git describe (make build VERSION=1.2.3 to set it)
make test       # unit tests
make e2e        # user journeys with the built binary in a throwaway home (needs git)
make lint       # golangci-lint
make dist       # dist/livery-<version>-<os>-<arch> for linux and darwin, and dist/SHA256SUMS
```

To cut a release, see [docs/release.md](docs/release.md).

Run all of them, and a vet of the macOS build, before you open a pull request:

```sh
make check
```

## Package map

- `cmd/livery`: the `main` package; calls `internal/cli`.
- `internal/adapter`: the contract between livery's core and one harness: the core computes what
  to install, an adapter says where it goes.
- `internal/adapter/claude`: the adapter for the Claude Code harness.
- `internal/adapter/importer`: the harness-neutral parts of `livery import` that the adapters share
  (skip list, skills folder walk, frontmatter helpers, ordered JSON reading); it knows no harness
  path or file name; it reads the `mcpServers` object shape that Claude Code and Kiro share.
- `internal/adapter/kiro`: the adapter for the Kiro harness (IDE and CLI share one folder).
- `internal/adapter/opencode`: the adapter for the opencode harness.
- `internal/apply`: writes what livery installs: files it owns and entries in shared JSON files.
- `internal/cli`: parses the command line and runs the chosen command.
- `internal/config`: livery's config file, one per scope: source, channel, teams, harnesses.
- `internal/content`: reads the shared repository's content: instructions, skills and so on.
- `internal/engine`: runs the parts in order: resolve the channel, load the layers, render for
  each harness and compute the change. It is the only core package that imports an adapter.
  `--project` runs it in project scope: the same steps, with the project root as the target.
- `internal/engine/enginetest`: test helpers: a fixture shared repository and an isolated Env.
- `internal/content/contenttest`: the small fixture shared repository (`contenttest.Source`, `contenttest.Base`) for adapter and content unit tests; they do not load `e2e/testdata/source`.
- `internal/exit`: the exit-code contract: 0 ok, 1 finding, 2 bad input (a flag, the config, or the content of the shared repository), 3 environment.
- `internal/lock`: reads and writes the lock file: the record of what livery installed and owns.
- `internal/paths`: where livery keeps its config, lock, personal layer and cache; in project
  scope, the `.livery/` folder of the project.
- `internal/secrets`: the secret scan behind `livery check` and `livery import`: the gitleaks rules
  as data (`rules.json`, `NOTICE`), the matcher (`secrets.go`) and the `secrets-allow` parser
  (`allow.go`).
- `internal/source`: resolves a channel to a commit and fetches the shared repository into the cache; it runs
  `git` as a subprocess.

## Add an adapter

An adapter supports one more harness. Put it in `internal/adapter/<name>`, the package name equal
to the harness name, and implement `adapter.Adapter` (`internal/adapter/adapter.go`):

```go
type Adapter interface {
	Name() string                                        // the harness name, equal to the package directory
	Root(env Env) string                                 // the harness's config folder, absolute
	Render(items []content.Item, env Env) (Plan, error)  // the files and JSON entries the harness needs
	EntryKeys(env Env) []EntryKeys                       // the keys livery owns in each shared MCP entry
}
```

`adapter.Env` says which scope runs: when `Env.Project` is empty the adapter renders for the
machine; when it holds the project root, `Root` and every path must lie inside that folder and
the harness environment variables (`KIRO_HOME`, ...) are not read.

`EntryKeys` must list every key the MCP rendering writes in an entry: livery owns only those keys
and leaves the user's other keys alone. Call `adaptertest.EntryKeysCover`
(`internal/adapter/adaptertest`) in the adapter's tests; it fails when `Render` writes a key
that `EntryKeys` does not list.

`Render` returns an `adapter.Plan`: the `Files` to write and the `Entries` to set in JSON files
the harness also writes. An adapter may also implement the optional `adapter.Importer` interface, which reads its
harness folder back into items for `livery import` (it only reads). Then register the adapter
in `Adapters` in `internal/engine/engine.go`; that is the only place a core package imports an
adapter package. Every harness path, file name and format (`.kiro`, `.claude`, ...) stays inside
the adapter package; the core must not know them. Add tests for the adapter and a journey in
`e2e/` if users can see the change. Document the harness, for both scopes, in `README.md` (the table "What is shared") and `docs/reference.md` ("Paths per harness").

## Pull requests

- One logical change per pull request.
- `make check` passes; CI runs the same steps (`make check` on Linux, `make test` and `make e2e`
  on macOS) and must be green. `govulncheck` runs as its own job and does not block a pull request.
- A change of behaviour comes with a test and the line in the docs that describes it.
- `main` takes changes only through pull requests; they are squash merged.

## Changes made with a coding agent

Welcome. The person who opens the pull request has read and run the change and answers for it.
The agent reads [AGENTS.md](AGENTS.md). Pull requests opened by an unattended agent are not
accepted.

## License

Contributions are under the Apache License 2.0, the license of the repository
([LICENSE](LICENSE)). There is no CLA and no sign-off.

## Releases

See [docs/release.md](docs/release.md). Cutting a release is the maintainer's job.
