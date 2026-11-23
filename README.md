# agent-switch

```text
   ___                    __     _____          _ __       __
  / _ | ___ ____ ___  ___/ /_   / ___/    __ __(_) /______/ /
 / __ |/ _ `/ -_) _ \/ _  / /   \__ \ |/|/ / // __/ __/ _  /
/_/ |_|\_, /\__/_//_/\_,_/_/   /____/__,__/_/\__/\__/\_,_/
      /___/

Switch AI coding agent accounts
https://github.com/annapo99/agent-switch

➤ 1. List       Show saved accounts
  2. Save       Save detected accounts
  3. Remove     Delete saved profile

List accounts

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

## Update

Update an installed release with:

```bash
ags update
```

When a newer GitHub Release exists, the terminal UI shows:

```text
Update v0.1.1 available, run ags update
```

`ags update` downloads the latest release archive for your OS and architecture,
verifies it with `checksums.txt`, and replaces the current `ags` binary.
Network failures are non-blocking for the TUI notice.

## Usage

Run `ags` to open the terminal UI:

```bash
ags
```

```text
Switch AI coding agent accounts
https://github.com/annapo99/agent-switch

➤ 1. List       Show saved accounts
  2. Save       Save detected accounts
  3. Remove     Delete saved profile

↑↓ Select  |  →/Enter Open  |  ←/B Back  |  Q Quit
```

Log in to Claude Code or Codex first, then open `ags` and choose `Save`. Saved
profiles get short per-agent numbers, so `claude #1` and `codex #1` can both
exist. Choose `List` to see saved profiles with the active account marked, then
select a profile to switch accounts.

Claude profiles are scoped by both account and organization. If the same email
belongs to Team and Enterprise workspaces, `ags save` stores each workspace under
its own Claude profile number.

Direct shell commands are available for scripting: `save`, `list`, `remove`,
`rename`, and `update`. The terminal UI `List` screen handles interactive
switching. `ags rename 1 work` shows profile #1 as `work` instead of its email
and organization; the name stays when the saved credentials are refreshed.

### Claude tokens

A long-lived token from `claude setup-token` can be saved as a Claude profile
without logging in through the browser. Pass the token on stdin and name the
profile:

```bash
security find-generic-password -s claude-token-bot -w | ags save --token --label bot
```

Switching to it stores the token as the Claude Keychain login, so every
`claude` command uses it without `CLAUDE_CODE_OAUTH_TOKEN`. Saving the same
label again replaces the token. A token only has the inference scope: it
cannot be refreshed and does not load claude.ai connectors, and MCP servers
that use OAuth need to be connected again on this profile. The usage endpoint
does not serve tokens, so `ags` reads a token profile's 5h and 7d usage from
the rate limit headers of a one-token Haiku request, at most every five
minutes; that request counts toward the account's usage.

### Claude background sessions

Claude Code background sessions (`claude --bg`, `claude agents`) run under a
daemon that keeps the account it started with. After switching Claude
accounts, `ags` restarts that daemon without stopping its sessions and
respawns every background session so it continues on the new account at once.
A session that is busy at that moment is restarted mid-task: its in-progress
reply or tool call is cut off, and shell commands it left running in the
background keep running without it. Send it a message to pick up where it
stopped. When `ags` runs inside a background session, that session is
restarted last, a couple of seconds after `ags` exits.

Usage metadata is best-effort; if a metadata source is unavailable, `ags` still
lists and switches saved accounts. Claude's usage endpoint answers about once a
minute per account, so when it refuses, `ags` shows the last usage it saw for
that account (or the newer copy Claude Code keeps for it) marked with its age,
such as `10m ago`.

## Supported Agents

| Agent | Current auth source | Saved profile source |
|---|---|---|
| Claude Code | macOS Keychain service `Claude Code-credentials`, then `~/.claude/.credentials.json` fallback | `~/.agent-switch/profiles/claude/<number>/` |
| Codex | `~/.codex/auth.json` | `~/.agent-switch/profiles/codex/<number>/` |

Claude usage metadata is enriched from saved Claude OAuth snapshots when a
valid access or refresh token is available. Codex usage metadata is enriched
from the ChatGPT Codex usage endpoint when a valid ChatGPT auth token is
available.

## Security Notes

`agent-switch` manages local credentials. Treat saved profiles as sensitive.

- It does not print token values.
- It stores credential snapshots under `~/.agent-switch/profiles/`.
- It hands Keychain credentials to `security` over stdin, so they stay out of
  the process list. When that does not work, or the credentials are too large
  for one input line (a few kilobytes), they are passed as an argument.
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

To publish a new version, push a tag:

```bash
git tag v0.1.1
git push origin v0.1.1
```

The release workflow builds signed-by-checksum release archives and publishes
them as the latest GitHub Release.

## Roadmap

- Add a Homebrew tap.
- Add more agent providers.
- Improve cross-platform credential storage.
- Add optional usage-aware switching.

## License

MIT
