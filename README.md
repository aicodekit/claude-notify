# Claude Code Notify

A lightweight webhook bridge that forwards [Claude Code](https://docs.anthropic.com/en/docs/claude-code) hook events to team messaging platforms in real-time. Single binary with self-install support.

## Supported Platforms

| Platform | Key Format |
|----------|-----------|
| WeChat Work (企业微信) | `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` (UUID, default) |
| Discord | `discord:https://discord.com/api/webhooks/xxx/yyy` |
| Telegram | `telegram:BOT_TOKEN:CHAT_ID` |
| Mattermost | `mattermost:https://mattermost.example.com/hooks/xxx` |
| Feishu (飞书) | `feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx` |
| DingTalk (钉钉) | `dingtalk:ACCESS_TOKEN:SECxxx...` |

## Quick Start

### 1. Build & Install

```bash
# Build from source (requires Go 1.22+)
make build

# Auto-install: copies binary to ~/.claude/hooks/ and configures hooks in settings.json
./dist/claude-notify install
```

### 2. Configure Notification Key

```bash
echo "YOUR_KEY_HERE" > ~/.claude/notify_key
```

Done! You'll now receive notifications when Claude Code processes your requests.

### Multi-Channel

One key per line. All channels receive notifications in parallel. Lines starting with `#` are ignored.

```
# notify to both Discord and Mattermost
discord:https://discord.com/api/webhooks/123456/abcdef
mattermost:https://mattermost.example.com/hooks/xxx
```

### Key Examples

```bash
# WeChat Work
echo "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" > ~/.claude/notify_key

# Discord
echo "discord:https://discord.com/api/webhooks/123456/abcdef" > ~/.claude/notify_key

# Telegram
echo "telegram:123456789:AAHdqTc-BOT-TOKEN:987654321" > ~/.claude/notify_key

# Mattermost
echo "mattermost:https://mattermost.example.com/hooks/xxx" > ~/.claude/notify_key

# Feishu
echo "feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx" > ~/.claude/notify_key

# DingTalk
echo "dingtalk:your_access_token:SECyour_secret" > ~/.claude/notify_key
```

## Install / Uninstall

```bash
claude-notify install             # Global: copy binary + add hooks to ~/.claude/settings.json
claude-notify install --project   # Project: add hooks to .claude/settings.json only
claude-notify uninstall           # Global: remove hooks + delete binary
claude-notify uninstall --project # Project: remove hooks from .claude/settings.json only
claude-notify version             # Show version
```

The install command automatically configures all 4 hook events: `UserPromptSubmit`, `PostToolUse`, `Stop`, `Notification`.

<details>
<summary>Manual configuration (alternative)</summary>

Add hooks to your Claude Code settings (`~/.claude/settings.json`):

```json
{
  "hooks": {
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "command": "~/.claude/hooks/claude-notify" }] }
    ],
    "PostToolUse": [
      { "hooks": [{ "type": "command", "command": "~/.claude/hooks/claude-notify" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "~/.claude/hooks/claude-notify" }] }
    ],
    "Notification": [
      { "hooks": [{ "type": "command", "command": "~/.claude/hooks/claude-notify" }] }
    ]
  }
}
```

</details>

## Notification Events

| Event | Icon | Description |
|-------|------|-------------|
| `UserPromptSubmit` | 👤 | User sent a prompt |
| `PostToolUse` | ⚡📖✏️🌐🔍🔎🤖📋 | Tool executed (icon varies by tool type) |
| `Stop` | ✅ | Task completed (mentions @all) |
| `Notification` | 🔔 | Claude needs attention |

### Tool Icons

| Icon | Tools |
|------|-------|
| ⚡ | Bash |
| 📖 | Read |
| ✏️ | Write, Edit, MultiEdit |
| 🌐 | WebFetch |
| 🔍 | WebSearch |
| 🔎 | Glob, Grep |
| 🤖 | Task |
| 📋 | TodoWrite, TodoRead |
| 🔧 | Other tools |

## Key Lookup Order

The binary searches for notification keys in this order (first found wins):

1. `<project_dir>/.claude/notify_key`
2. `~/.claude/notify_key`
3. `<project_dir>/.claude/wecom_key` (legacy)
4. `~/.claude/wecom_key` (legacy)

If no key is found, the program exits silently.

## Build from Source

Requires Go 1.22+.

```bash
make build    # Current platform → dist/claude-notify
make release  # Cross-compile all platforms → dist/claude-notify-{os}-{arch}
make clean    # Clean dist/
```

## How It Works

```
Claude Code Hook Event
        │
        ▼
   stdin (JSON)
        │
        ▼
  ┌─────────────┐
  │ Parse Event  │
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐     .claude/notify_key
  │ Lookup Key  │◄─── ~/.claude/notify_key
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │ Detect      │── wecom (default)
  │ Platform    │── discord:
  │ from Key    │── telegram:
  │ Prefix      │── mattermost:
  │             │── feishu:
  └──────┬──────┘── dingtalk:
         │
         ▼
  ┌─────────────┐
  │ Format &    │
  │ Send        │──► Platform Webhook API (parallel)
  └─────────────┘
```

## License

MIT
