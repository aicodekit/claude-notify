# Claude Code Notify

轻量级 Webhook 桥接工具，将 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 的 Hook 事件实时转发到团队通讯平台。单文件二进制，支持自安装。

## 支持平台

| 平台 | Key 格式 |
|------|---------|
| 企业微信 | `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`（UUID，默认） |
| Discord | `discord:https://discord.com/api/webhooks/xxx/yyy` |
| Telegram | `telegram:BOT_TOKEN:CHAT_ID` |
| Mattermost | `mattermost:https://mattermost.example.com/hooks/xxx` |
| 飞书 | `feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx` |
| 钉钉 | `dingtalk:ACCESS_TOKEN:SECxxx...` |

## 快速开始

### 1. 构建并安装

```bash
# 从源码构建（需要 Go 1.22+）
make build

# 自动安装：复制二进制到 ~/.claude/hooks/ 并配置 settings.json
./dist/claude-notify install
```

### 2. 配置通知密钥

```bash
echo "YOUR_KEY_HERE" > ~/.claude/notify_key
```

完成！Claude Code 处理任务时，你将实时收到通知。

### 多渠道

每行一个 key，所有渠道并行接收通知。`#` 开头的行为注释。

```
# 同时发送到 Discord 和 Mattermost
discord:https://discord.com/api/webhooks/123456/abcdef
mattermost:https://mattermost.example.com/hooks/xxx
```

### 各平台 Key 示例

```bash
# 企业微信
echo "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" > ~/.claude/notify_key

# Discord
echo "discord:https://discord.com/api/webhooks/123456/abcdef" > ~/.claude/notify_key

# Telegram
echo "telegram:123456789:AAHdqTc-BOT-TOKEN:987654321" > ~/.claude/notify_key

# Mattermost
echo "mattermost:https://mattermost.example.com/hooks/xxx" > ~/.claude/notify_key

# 飞书
echo "feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx" > ~/.claude/notify_key

# 钉钉
echo "dingtalk:your_access_token:SECyour_secret" > ~/.claude/notify_key
```

## 安装 / 卸载

```bash
claude-notify install             # 全局：复制二进制 + 配置 ~/.claude/settings.json
claude-notify install --project   # 项目级：仅配置 .claude/settings.json
claude-notify uninstall           # 全局：移除 hooks + 删除二进制
claude-notify uninstall --project # 项目级：仅移除 .claude/settings.json 中的 hooks
claude-notify version             # 显示版本号
```

install 命令自动配置全部 4 个 Hook 事件：`UserPromptSubmit`、`PostToolUse`、`Stop`、`Notification`。

<details>
<summary>手动配置（备选方案）</summary>

在 Claude Code 设置文件（`~/.claude/settings.json`）中添加：

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

## 通知事件

| 事件 | 图标 | 说明 |
|------|------|------|
| `UserPromptSubmit` | 👤 | 用户发送了提示词 |
| `PostToolUse` | ⚡📖✏️🌐🔍🔎🤖📋 | 工具执行完成（图标随工具类型变化） |
| `Stop` | ✅ | 任务完成（@所有人） |
| `Notification` | 🔔 | Claude 需要关注 |

### 工具图标

| 图标 | 工具 |
|------|------|
| ⚡ | Bash |
| 📖 | Read |
| ✏️ | Write、Edit、MultiEdit |
| 🌐 | WebFetch |
| 🔍 | WebSearch |
| 🔎 | Glob、Grep |
| 🤖 | Task |
| 📋 | TodoWrite、TodoRead |
| 🔧 | 其他工具 |

## 密钥查找顺序

程序按以下顺序查找通知密钥（找到即停止）：

1. `<项目目录>/.claude/notify_key`
2. `~/.claude/notify_key`
3. `<项目目录>/.claude/wecom_key`（旧版兼容）
4. `~/.claude/wecom_key`（旧版兼容）

如果未找到密钥，程序将静默退出。

## 从源码构建

需要 Go 1.22+。

```bash
make build    # 当前平台 → dist/claude-notify
make release  # 交叉编译所有平台 → dist/claude-notify-{os}-{arch}
make clean    # 清理 dist/
```

## 工作原理

```
Claude Code Hook 事件
        │
        ▼
   stdin (JSON)
        │
        ▼
  ┌─────────────┐
  │  解析事件    │
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐     .claude/notify_key
  │  查找密钥    │◄─── ~/.claude/notify_key
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │  识别平台    │── 企业微信（默认）
  │  （根据Key   │── discord:
  │   前缀）     │── telegram:
  │             │── mattermost:
  │             │── feishu:
  └──────┬──────┘── dingtalk:
         │
         ▼
  ┌─────────────┐
  │  格式化并    │
  │  发送通知    │──► 平台 Webhook API（并行）
  └─────────────┘
```

## 许可证

MIT
