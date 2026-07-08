# agent-switch

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

Current accounts

Claude
  2: annapo.claude@example.com [Example Team] (active)
     ├ 5h       █████░░░░░  48%   resets 18:40         in 2h 15m
     ├ 7d       ███░░░░░░░  27%   resets Jul 13 02:59  in 4d 10h
     ├ Fable    ██░░░░░░░░  15%   resets Jul 13 02:59  in 4d 10h
     ├ • oauth: fresh, refresh token yes, expires 21:48 in 5h 23m
     └ • plan: claude max 5x

Codex
  1: annapo.codex@example.com (active)
     ├ 5h       █░░░░░░░░░  12%   resets 18:47         in 2h 22m
     ├ 7d       █░░░░░░░░░  10%   resets Jul 14 12:00  in 5d 19h
     ├ Spark 5h ░░░░░░░░░░   0%   resets 21:24         in 5h 0m
     ├ Spark 7d ░░░░░░░░░░   0%   resets Jul 15 16:24  in 7d 0h
     ├ • oauth: fresh, refresh token yes, expires 20:34 in 4h 10m
     └ • plan: pro
```

`agent-switch` is a local CLI for saving and switching authentication profiles
used by AI coding agents. It currently supports Claude Code and Codex.

## Download

Recommended install path for macOS and Linux:

```bash
curl -fsSLO https://github.com/annapo99/agent-switch/releases/latest/download/install.sh
sh install.sh
```

The installer downloads a GitHub Release binary into `~/.local/bin` and verifies
the binary against `checksums.txt` before installing it. The script is saved
locally first so you can review it before running it.

To install somewhere else, run `AGS_INSTALL_DIR=/usr/local/bin sh install.sh`.
With Go installed, `go install github.com/annapo99/agent-switch/cmd/ags@latest`
also works.

To verify the installer before running it, download `checksums.txt` from the
same release and run `grep ' install.sh$' checksums.txt | shasum -a 256 -c -`.

## Usage

Run `ags` to open the terminal UI:

```bash
ags
```

```text
Switch AI coding agent accounts
https://github.com/annapo99/agent-switch

➤ 1. Current    Show active accounts
  2. List       Browse saved accounts
  3. Save       Save detected accounts
  4. Use        Switch account
  5. Remove     Delete saved profile

↑↓ Select  |  →/Enter Open  |  ←/B Back  |  Q Quit
```

Log in to Claude Code or Codex first, then open `ags` and choose `Save`. Saved
profiles get short per-agent numbers, so `claude #1` and `codex #1` can both
exist. Choose `Use` to switch accounts and `Current` to see the active saved
profiles.

The same workflows are available as direct shell commands for scripting:
`save`, `use`, `list`, `current`, and `remove`. Usage metadata is best-effort;
if a metadata source is unavailable, `ags` still lists and switches saved
accounts.

## Supported Agents

| Agent | Current auth source | Saved profile source |
|---|---|---|
| Claude Code | macOS Keychain service `Claude Code-credentials`, then `~/.claude/.credentials.json` fallback | `~/.agent-switch/profiles/claude/<number>/` |
| Codex | `~/.codex/auth.json` | `~/.agent-switch/profiles/codex/<number>/` |

Claude usage metadata is enriched from `claude-swap` when `cswap` is installed.
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

```bash
git clone git@github.com:annapo99/agent-switch.git
cd agent-switch
go test ./...
go run ./cmd/ags
```

## Roadmap

- Add a Homebrew tap.
- Add more agent providers.
- Improve cross-platform credential storage.
- Add optional usage-aware switching.

## License

MIT
