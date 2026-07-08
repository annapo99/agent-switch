# agent-switch

## Interactive Menu

Running `ags` without arguments opens a terminal UI.

```text
   ___                    __     _____          _ __       __
  / _ | ___ ____ ___  ___/ /_   / ___/    __ __(_) /______/ /
 / __ |/ _ `/ -_) _ \/ _  / /   \__ \ |/|/ / // __/ __/ _  /
/_/ |_|\_, /\__/_//_/\_,_/_/   /____/__,__/_/\__/\__/\_,_/
      /___/

Switch AI coding agent accounts
https://github.com/annapo99/agent-switch

➤ 1. Current    Show active accounts
  2. List       Browse saved accounts
  3. Save       Save detected accounts
  4. Use        Switch account
  5. Remove     Delete saved profile

↑↓ Select  |  →/Enter Open  |  ←/B Back  |  Q Quit
```

The direct commands remain available for fast shell usage.

`agent-switch` is a local CLI for saving and switching authentication profiles
used by AI coding agents.

The command is `ags`.

```bash
ags
ags save
ags list
ags use 1
ags current
```

It currently supports Claude Code and Codex. More agent providers can be added
without changing the command shape.

## Why

AI coding tools usually keep login state in local files or the macOS Keychain.
If you use several accounts, switching often means logging out, logging back in,
or moving credentials around by hand.

`agent-switch` snapshots the current local auth state into numbered profiles and
restores the selected profile when you want to switch.

## Features

- Save the currently active Claude Code or Codex account.
- Switch accounts with short per-agent numbers like `ags use 2`.
- Detect duplicate saved accounts by credential fingerprint.
- Show active accounts, organizations, usage windows, OAuth freshness, and plan
  metadata when available.
- Keep Claude and Codex numbering separate, so `claude #1` and `codex #1` can
  both exist.
- Runs as a single Go binary. No Python, Ruby, Node, or shell wrapper required.
- Includes an interactive TUI for account browsing and switching.

## Install

Recommended install path for macOS and Linux:

```bash
VERSION=v0.1.0
BASE="https://github.com/annapo99/agent-switch/releases/download/${VERSION}"
curl -fsSLO "${BASE}/install.sh"
curl -fsSLO "${BASE}/checksums.txt"
grep ' install.sh$' checksums.txt | shasum -a 256 -c -
AGS_VERSION="${VERSION}" sh install.sh
```

Then run:

```bash
ags --help
```

The installer downloads a GitHub Release binary into `~/.local/bin` and verifies
the binary against `checksums.txt` before installing it. This avoids piping
remote code directly into the shell; review `install.sh` before running it if
you want to inspect the installer first.

To install somewhere else:

```bash
AGS_INSTALL_DIR=/usr/local/bin AGS_VERSION=v0.1.0 sh install.sh
```

With Go installed, you can also install from source:

```bash
go install github.com/annapo99/agent-switch/cmd/ags@latest
```

For local development:

```bash
git clone git@github.com:annapo99/agent-switch.git
cd agent-switch
go run ./cmd/ags --help
```

Release binaries are published when a version tag is pushed:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Quick Start

Open the interactive menu:

```bash
ags
```

Log in to Claude Code or Codex first, then save the current account:

```bash
ags save
```

List saved profiles:

```bash
ags list
```

Switch to a saved profile:

```bash
ags use 1
```

Show the active saved profiles:

```bash
ags current
```

Remove a saved profile:

```bash
ags remove 1
```

Show help:

```bash
ags --help
```

## Commands

| Command | Description |
|---|---|
| `ags save` | Save detected active agent accounts. |
| `ags use <number>` | Switch to a saved profile number. |
| `ags list` | List saved profiles grouped by agent. |
| `ags current` | Show saved profiles that match the current active auth state. |
| `ags remove <number>` | Remove a saved profile. |

Common options:

| Option | Commands | Description |
|---|---|---|
| `--agent claude` | all commands | Limit the command to Claude. |
| `--agent codex` | all commands | Limit the command to Codex. |
| `--yes`, `-y` | `save`, `use`, `remove` | Skip confirmation where supported. |
| `--json` | `list`, `current` | Print machine-readable JSON. |

## Colors

`ags` uses colored output when stdout is an interactive terminal.

If a wrapper or shell sets `TERM=dumb`, force colors with:

```bash
AGS_COLOR=always ags list
```

To disable colors explicitly:

```bash
AGS_COLOR=never ags list
```

`NO_COLOR=1` also disables colors in automatic mode:

```bash
NO_COLOR=1 ags list
```

## Save Behavior

If one new account is detected, `ags save` asks once. Pressing Enter means yes.

```text
Detected Claude account

  annapo.claude@example.com [Example Team]
     └ save as #1

Save this account? [Y/n]
```

If multiple active accounts are detected, `ags save` shows one grouped list.
Pressing Enter saves all detected new accounts.

```text
Detected active agent accounts

Claude
  1) annapo.claude@example.com [Example Team]
     └ save as #1
Codex
  2) annapo.codex@example.com
     └ save as #1

Which account should be saved? [1/2 Enter save all]
```

Already-saved accounts are not saved again.

## Listing Accounts

`ags list` renders saved accounts as a small tree. Active accounts are marked
with `(active)`.

```text
Saved accounts

Claude
  2: annapo@example.com [Example Team] (active)
     ├ 5h       ░░░░░░░░░░   2%   resets 18:40         in 4h 40m
     ├ 7d       ██░░░░░░░░  21%   resets Jul 13 03:00  in 4d 13h
     ├ Fable    █░░░░░░░░░   9%   resets Jul 13 03:00  in 4d 13h
     ├ • oauth: fresh, refresh token yes, expires 21:48 in 7h 49m
     └ • plan: claude max 5x

Codex
  1: annapo.codex@example.com (active)
     ├ 5h       ░░░░░░░░░░   1%   resets 18:47         in 4h 48m
     ├ 7d       █░░░░░░░░░   8%   resets Jul 14 12:00  in 5d 22h
     ├ Spark 5h ░░░░░░░░░░   0%   resets 18:59         in 5h 0m
     ├ Spark 7d ░░░░░░░░░░   0%   resets Jul 15 13:59  in 7d 0h
     ├ • oauth: fresh, refresh token yes, expires 20:34 in 6h 30m
     └ • plan: pro
```

Usage metadata is best-effort. If a metadata source is unavailable, `ags` still
lists and switches saved accounts.

## Agent-Scoped Numbers

Numbers are scoped per agent:

```text
claude #1
claude #2
codex  #1
```

If a number only matches one saved profile, `ags use <number>` switches
immediately.

If the same number exists for several agents, `ags` asks one disambiguation
question:

```text
Multiple accounts match #1

  1  claude  annapo@example.com
  2  codex   annapo@example.com

Which account should be used? [1/2/N, Enter cancels]
```

Use `--agent` to avoid ambiguity:

```bash
ags use 1 --agent claude
ags list --agent codex
ags remove 2 --agent claude
```

## Supported Agents

| Agent | Current auth source | Saved profile source |
|---|---|---|
| Claude Code | macOS Keychain service `Claude Code-credentials`, then `~/.claude/.credentials.json` fallback | `~/.agent-switch/profiles/claude/<number>/` |
| Codex | `~/.codex/auth.json` | `~/.agent-switch/profiles/codex/<number>/` |

Claude usage metadata is enriched from `claude-swap` when `cswap` is installed:

```bash
cswap list --json
cswap list --token-status
```

Codex usage metadata is enriched from the ChatGPT Codex usage endpoint when a
valid ChatGPT auth token is available.

## Security Notes

`agent-switch` manages local credentials. Treat saved profiles as sensitive.

- It does not print token values.
- It stores credential snapshots under `~/.agent-switch/profiles/`.
- It may store Claude Keychain snapshots in profile directories so they can be
  restored later.
- Do not commit `~/.agent-switch`, `~/.claude`, `~/.codex`, `.claude.json`,
  `auth.json`, `keychain.json`, `.env`, or key files.
- This repository's `.gitignore` blocks those common local credential paths.

Use full-disk encryption and normal OS account protections if you save agent
profiles on a shared machine.

## Development

Run tests:

```bash
go test ./...
```

Run from source:

```bash
go run ./cmd/ags list
```

Build a local binary:

```bash
go build ./cmd/ags
./ags --help
```

## Roadmap

- Add GitHub Release binaries for macOS and Linux.
- Add a Homebrew formula.
- Add more agent providers.
- Improve cross-platform credential storage.
- Add optional usage-aware switching.

## License

MIT
