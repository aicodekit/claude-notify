# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Code webhook notification bridge — a single-file Go program that receives Claude Code hook events via stdin (JSON) and forwards formatted notifications to team messaging platforms. Dependencies: `tidwall/gjson` + `tidwall/sjson` for order-preserving JSON manipulation in install/uninstall.

## Supported Platforms

| Platform | Key format | Character limit |
|----------|-----------|----------------|
| WeChat Work (企业微信) | UUID (default) | 3800 bytes |
| Discord | `discord:<webhook_url>` | 2000 bytes |
| Telegram | `telegram:<bot_token>:<chat_id>` | 4000 bytes |
| Mattermost | `mattermost:<webhook_url>` | 4000 bytes |
| Feishu (飞书) | `feishu:<webhook_url>` | 4000 bytes |
| DingTalk (钉钉) | `dingtalk:<access_token>:<secret>` | 4000 bytes |

## Architecture

Single file `main.go`. Core pattern is a **Sender interface** (Strategy pattern) with per-platform implementations. Each Sender handles bold formatting, @all mentions, and HTTP POST delivery with platform-specific payload schemas.

**Subcommand routing**: `os.Args` checked before stdin read. `install`/`uninstall`/`version` are subcommands; no args = webhook mode (reads stdin).

**Webhook flow**: `stdin JSON → HookPayload → key lookup → []Sender → buildMsg per sender → parallel Send()`

**Hook events handled**: `UserPromptSubmit`, `PostToolUse`, `Stop`, `Notification`

**Key lookup order** (first non-empty file wins, supports multi-line for multi-channel):
1. `<project>/.claude/notify_key`
2. `~/.claude/notify_key`
3. `<project>/.claude/wecom_key` (legacy)
4. `~/.claude/wecom_key` (legacy)

**Assistant message dedup**: Uses `/tmp/claude_hook_last_<sessionid>` to avoid re-sending the same assistant text within a session.

## Build

```bash
make build    # Current platform → dist/claude-notify
make release  # Cross-compile → dist/claude-notify-{os}-{arch}
make clean    # Clean dist/
```

Version is injected via `-ldflags "-X main.Version=..."` (timestamp).

## Install / Uninstall

```bash
./dist/claude-notify install             # Copy to ~/.claude/hooks/ + add hooks to ~/.claude/settings.json
./dist/claude-notify install --project   # Add hooks to .claude/settings.json (project-level)
./dist/claude-notify uninstall           # Remove hooks + delete binary
./dist/claude-notify uninstall --project # Remove project-level hooks only
```

JSON manipulation uses `sjson.SetRaw` / `sjson.Delete` to preserve key order in settings.json.

## Platform-Specific Notes

- **DingTalk**: Uses HMAC-SHA256 signing (timestamp + secret). The `@all` mention goes through the `isAtAll` JSON field, not inline text.
- **Telegram**: Uses MarkdownV2 parse mode — all special characters must be escaped via `tgEscape()`.
- **Feishu**: Uses interactive card format (`msg_type: "interactive"`) with `lark_md` content tag.
