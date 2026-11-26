# Security policy

## Supported version

The latest release of livery
([releases](https://github.com/Pnakoryn/livery/releases)). Older releases get no fixes.

## Report a vulnerability

Do not open a public issue. Use the "Report a vulnerability" button on the Security tab of
[github.com/Pnakoryn/livery](https://github.com/Pnakoryn/livery/security) (GitHub private
vulnerability reporting).

Include: the livery version (`livery version`), your OS, the command you ran, what happened and
what you expected, and the steps or a small shared repository that reproduce it.

## What livery promises

- The only program it runs is `git`; the network is used only for git fetches of the configured
  source.
- It writes into the harness folders and their MCP config files (with `--project`: only below the
  project root), its own config, lock and cache folders, and the folder you name with
  `livery import --into`.
- It never follows a symbolic link and never overwrites a file it does not own.
- `livery check` and `livery import` scan for secrets before anything is published.
