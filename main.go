package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var Version = "dev"

// ── Claude Code Hook payload ──────────────────────────────

type HookPayload struct {
	HookEventName  string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	CWD            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	Prompt         string          `json:"prompt"`
	Message        string          `json:"message"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
}

// ── Transcript JSONL ─────────────────────────────────────

type TranscriptLine struct {
	Role    string            `json:"role"`
	Content []TranscriptBlock `json:"content"`
	Message *TranscriptMsg    `json:"message"`
}

type TranscriptMsg struct {
	Role    string            `json:"role"`
	Content []TranscriptBlock `json:"content"`
}

type TranscriptBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ── 平台接口 ─────────────────────────────────────────────

type Sender interface {
	B(s string) string  // 粗体
	MentionAll() string // @所有人
	Send(text string)   // 发送
}

// ── 企业微信 ──────────────────────────────────────────────

type WeComSender struct{ url string }

func (w *WeComSender) B(s string) string { return "**" + s + "**" }
func (w *WeComSender) MentionAll() string { return "<@all>" }
func (w *WeComSender) Send(text string) {
	text = truncateBytes(text, 3800)
	type md struct {
		Content string `json:"content"`
	}
	body, _ := json.Marshal(struct {
		MsgType  string `json:"msgtype"`
		Markdown md     `json:"markdown"`
	}{"markdown", md{text}})
	httpPost(w.url, body)
}

// ── Discord ───────────────────────────────────────────────

type DiscordSender struct{ url string }

func (d *DiscordSender) B(s string) string { return "**" + s + "**" }
func (d *DiscordSender) MentionAll() string { return "@everyone" }
func (d *DiscordSender) Send(text string) {
	text = truncateBytes(text, 2000)
	body, _ := json.Marshal(map[string]string{"content": text})
	httpPost(d.url, body)
}

// ── Telegram ──────────────────────────────────────────────

type TelegramSender struct{ token, chatID string }

func (t *TelegramSender) B(s string) string { return "*" + tgEscape(s) + "*" }
func (t *TelegramSender) MentionAll() string { return "" }
func (t *TelegramSender) Send(text string) {
	text = truncateBytes(text, 4000)
	apiURL := "https://api.telegram.org/bot" + t.token + "/sendMessage"
	body, _ := json.Marshal(map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	})
	httpPost(apiURL, body)
}

func tgEscape(s string) string {
	r := strings.NewReplacer(
		"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]",
		"(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`",
		">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
		"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}",
		".", "\\.", "!", "\\!",
	)
	return r.Replace(s)
}

// ── Mattermost ────────────────────────────────────────────
// 文档：https://developers.mattermost.com/integrate/webhooks/incoming/
// key 格式：mattermost:https://mattermost.example.com/hooks/xxx

type MattermostSender struct{ url string }

func (m *MattermostSender) B(s string) string { return "**" + s + "**" }
func (m *MattermostSender) MentionAll() string { return "@all" }
func (m *MattermostSender) Send(text string) {
	text = truncateBytes(text, 4000)
	body, _ := json.Marshal(map[string]string{"text": text})
	httpPost(m.url, body)
}

// ── 飞书 ──────────────────────────────────────────────────
// 文档：https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
// 飞书 webhook 支持 markdown，粗体用 **text**

type FeishuSender struct{ url string }

func (f *FeishuSender) B(s string) string { return "**" + s + "**" }
func (f *FeishuSender) MentionAll() string { return "<at user_id=\"all\">所有人</at>" }
func (f *FeishuSender) Send(text string) {
	text = truncateBytes(text, 4000)
	body, _ := json.Marshal(map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"elements": []map[string]interface{}{
				{
					"tag": "div",
					"text": map[string]string{
						"content": text,
						"tag":     "lark_md",
					},
				},
			},
		},
	})
	httpPost(f.url, body)
}

// ── 钉钉 ──────────────────────────────────────────────────
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access
// 钉钉自定义机器人需要 HMAC-SHA256 签名
// key 格式：dingtalk:ACCESS_TOKEN:SECRET
// SECRET 以 "SEC" 开头

type DingTalkSender struct{ token, secret string }

func (d *DingTalkSender) B(s string) string { return "**" + s + "**" }
func (d *DingTalkSender) MentionAll() string { return "" } // 通过 isAtAll 字段实现
func (d *DingTalkSender) Send(text string) {
	text = truncateBytes(text, 4000)
	isAtAll := strings.Contains(text, "@all_placeholder")
	text = strings.ReplaceAll(text, "@all_placeholder", "")

	// 生成签名
	ts := time.Now().UnixMilli()
	sign := d.sign(ts)
	apiURL := fmt.Sprintf(
		"https://oapi.dingtalk.com/robot/send?access_token=%s&timestamp=%d&sign=%s",
		d.token, ts, url.QueryEscape(sign),
	)

	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": "Claude Code",
			"text":  text,
		},
		"at": map[string]interface{}{
			"isAtAll": isAtAll,
		},
	})
	httpPost(apiURL, body)
}

func (d *DingTalkSender) sign(ts int64) string {
	msg := fmt.Sprintf("%d\n%s", ts, d.secret)
	mac := hmac.New(sha256.New, []byte(d.secret))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ── 工厂：从 key 内容识别平台 ────────────────────────────
//
// wecom      → xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
// discord    → discord:https://discord.com/api/webhooks/xxx/yyy
// telegram   → telegram:BOT_TOKEN:CHAT_ID
// mattermost → mattermost:https://mattermost.example.com/hooks/xxx
// feishu     → feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx
// dingtalk   → dingtalk:ACCESS_TOKEN:SECxxx...

func newSender(key string) Sender {
	switch {
	case strings.HasPrefix(key, "discord:"):
		return &DiscordSender{url: strings.TrimPrefix(key, "discord:")}
	case strings.HasPrefix(key, "telegram:"):
		parts := strings.SplitN(strings.TrimPrefix(key, "telegram:"), ":", 2)
		if len(parts) == 2 {
			return &TelegramSender{token: parts[0], chatID: parts[1]}
		}
	case strings.HasPrefix(key, "mattermost:"):
		return &MattermostSender{url: strings.TrimPrefix(key, "mattermost:")}
	case strings.HasPrefix(key, "feishu:"):
		return &FeishuSender{url: strings.TrimPrefix(key, "feishu:")}
	case strings.HasPrefix(key, "dingtalk:"):
		parts := strings.SplitN(strings.TrimPrefix(key, "dingtalk:"), ":", 2)
		if len(parts) == 2 {
			return &DingTalkSender{token: parts[0], secret: parts[1]}
		}
	}
	// 默认企业微信
	return &WeComSender{url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + key}
}

// ── 工具函数 ──────────────────────────────────────────────

func httpPost(apiURL string, body []byte) {
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	b := []byte(s)[:n]
	for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 {
		b = b[:len(b)-1]
	}
	return string(b) + "…"
}

func readKeys(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	return keys
}

func baseName(path string) string { return filepath.Base(path) }

func getField(raw json.RawMessage, fields ...string) string {
	if raw == nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, f := range fields {
		if v, ok := m[f]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return s
			}
			return string(v)
		}
	}
	return ""
}

func firstValue(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, v := range m {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
		return string(v)
	}
	return ""
}

func exitCode(raw json.RawMessage) int {
	if raw == nil {
		return 0
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return 0
	}
	if v, ok := m["exit_code"]; ok {
		var c int
		if json.Unmarshal(v, &c) == nil {
			return c
		}
	}
	return 0
}

func lastAssistantText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	last := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var tl TranscriptLine
		if json.Unmarshal([]byte(line), &tl) != nil {
			continue
		}
		role, content := tl.Role, tl.Content
		if tl.Message != nil {
			role, content = tl.Message.Role, tl.Message.Content
		}
		if role != "assistant" {
			continue
		}
		var parts []string
		for _, b := range content {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, strings.TrimSpace(b.Text))
			}
		}
		if len(parts) > 0 {
			last = strings.Join(parts, " ")
		}
	}
	return last
}

// ── main ─────────────────────────────────────────────────

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "install":
			cmdInstall(hasFlag(os.Args[2:], "--project"))
			return
		case "uninstall":
			cmdUninstall(hasFlag(os.Args[2:], "--project"))
			return
		case "version", "--version", "-v":
			fmt.Println("claude-notify " + Version)
			return
		}
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil || len(data) == 0 {
		os.Exit(0)
	}

	var p HookPayload
	if json.Unmarshal(data, &p) != nil {
		os.Exit(0)
	}

	// key 查找：项目级 > 全局，notify_key 优先，兼容旧 wecom_key
	// 支持多行多渠道：每行一个 key，并行发送
	home, _ := os.UserHomeDir()
	proj := p.CWD
	if proj == "" {
		proj, _ = os.Getwd()
	}
	var keys []string
	for _, path := range []string{
		filepath.Join(proj, ".claude", "notify_key"),
		filepath.Join(home, ".claude", "notify_key"),
		filepath.Join(proj, ".claude", "wecom_key"),
		filepath.Join(home, ".claude", "wecom_key"),
	} {
		if keys = readKeys(path); len(keys) > 0 {
			break
		}
	}
	if len(keys) == 0 {
		os.Exit(0)
	}

	var senders []Sender
	for _, k := range keys {
		senders = append(senders, newSender(k))
	}

	tag := baseName(p.CWD)
	if tag == "" || tag == "." {
		if len(p.SessionID) >= 8 {
			tag = p.SessionID[:8]
		} else {
			tag = p.SessionID
		}
	}

	// buildMsg 为每个 sender 生成消息（因为粗体/mention 格式因平台而异）
	// 返回 nil 表示无需发送
	buildMsg := func(s Sender) []string {
		btag := s.B("[" + tag + "]")

		switch p.HookEventName {

		case "UserPromptSubmit":
			prompt := p.Prompt
			if prompt == "" {
				prompt = p.Message
			}
			return []string{fmt.Sprintf("👤 %s\n%s", btag, truncate(prompt, 40))}

		case "PostToolUse":
			tool := p.ToolName
			icon, param := "🔧", ""

			switch tool {
			case "Bash":
				icon = "⚡"
				cmd := getField(p.ToolInput, "command")
				if i := strings.Index(cmd, "\n"); i >= 0 {
					cmd = cmd[:i]
				}
				param = truncate(cmd, 60)
				if c := exitCode(p.ToolResponse); c != 0 {
					param = fmt.Sprintf("%s  ❌exit=%d", param, c)
				}
			case "Read":
				icon = "📖"
				param = baseName(getField(p.ToolInput, "file_path"))
			case "Write", "Edit", "MultiEdit":
				icon = "✏️"
				param = baseName(getField(p.ToolInput, "file_path", "path"))
			case "WebFetch":
				icon = "🌐"
				param = truncate(getField(p.ToolInput, "url"), 60)
			case "WebSearch":
				icon = "🔍"
				param = truncate(getField(p.ToolInput, "query"), 60)
			case "Glob", "Grep":
				icon = "🔎"
				param = truncate(getField(p.ToolInput, "pattern"), 40)
			case "Task":
				icon = "🤖"
				param = truncate(getField(p.ToolInput, "description"), 60)
			case "TodoWrite", "TodoRead":
				icon = "📋"
				param = truncate(firstValue(p.ToolInput), 40)
			default:
				param = truncate(baseName(firstValue(p.ToolInput)), 60)
			}

			msg := fmt.Sprintf("%s %s %s", icon, btag, tool)
			if param != "" {
				msg = fmt.Sprintf("%s %s %s(%s)", icon, btag, tool, param)
			}

			msgs := []string{msg}

			// 助手中间消息（去重，仅计算一次）
			if p.TranscriptPath != "" {
				text := truncate(lastAssistantText(p.TranscriptPath), 150)
				if text != "" {
					stateFile := filepath.Join(os.TempDir(), "claude_hook_last_"+p.SessionID)
					lastSent, _ := os.ReadFile(stateFile)
					if text != string(lastSent) {
						os.WriteFile(stateFile, []byte(text), 0600)
						msgs = append(msgs, fmt.Sprintf("💬 %s\n%s", btag, text))
					}
				}
			}
			return msgs

		case "Stop":
			reply := ""
			if p.TranscriptPath != "" {
				reply = truncate(lastAssistantText(p.TranscriptPath), 200)
			}
			mention := s.MentionAll()
			if mention == "" {
				if _, ok := s.(*DingTalkSender); ok {
					mention = "@all_placeholder"
				}
			}
			suffix := ""
			if mention != "" {
				suffix = " " + mention
			}
			msg := fmt.Sprintf("✅ %s 任务完成%s", btag, suffix)
			if reply != "" {
				msg += "\n" + reply
			}
			return []string{msg}

		case "Notification":
			return []string{fmt.Sprintf("🔔 %s 需要关注\n%s", btag, truncate(p.Message, 200))}
		}

		return nil
	}

	// 并行发送到所有渠道
	var wg sync.WaitGroup
	for _, s := range senders {
		wg.Add(1)
		go func(s Sender) {
			defer wg.Done()
			for _, msg := range buildMsg(s) {
				s.Send(msg)
			}
		}(s)
	}
	wg.Wait()

	os.Exit(0)
}

// ── install / uninstall ─────────────────────────────────

const (
	installDir  = ".claude/hooks"
	binaryName  = "claude-notify"
	hookCommand = "~/.claude/hooks/claude-notify"
)

var hookEvents = []string{"UserPromptSubmit", "PostToolUse", "Stop", "Notification"}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func cmdInstall(projectMode bool) {
	// 全局模式始终拷贝；项目模式检测目标不存在时也拷贝
	needCopy := !projectMode
	if projectMode {
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, installDir, binaryName)); os.IsNotExist(err) {
			needCopy = true
		}
	}
	if needCopy {
		if err := installBinary(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	settingsPath := settingsFilePath(projectMode)
	if err := addHooksToSettings(settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ claude-notify installed successfully!")
	if needCopy {
		home, _ := os.UserHomeDir()
		fmt.Printf("  Binary: %s\n", filepath.Join(home, installDir, binaryName))
	}
	fmt.Printf("  Hooks:  %s\n", settingsPath)
	fmt.Printf("  Events: %s\n", strings.Join(hookEvents, ", "))

	keyPath := "~/.claude/notify_key"
	if projectMode {
		keyPath = ".claude/notify_key"
	}
	fmt.Printf("\nConfigure your notification key (%s):\n", keyPath)
	fmt.Println("")
	fmt.Printf("  # WeChat Work\n  echo \"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx\" > %s\n\n", keyPath)
	fmt.Printf("  # Discord\n  echo \"discord:https://discord.com/api/webhooks/xxx/yyy\" > %s\n\n", keyPath)
	fmt.Printf("  # Telegram\n  echo \"telegram:BOT_TOKEN:CHAT_ID\" > %s\n\n", keyPath)
	fmt.Printf("  # Mattermost\n  echo \"mattermost:https://mattermost.example.com/hooks/xxx\" > %s\n\n", keyPath)
	fmt.Printf("  # Feishu\n  echo \"feishu:https://open.feishu.cn/open-apis/bot/v2/hook/xxx\" > %s\n\n", keyPath)
	fmt.Printf("  # DingTalk\n  echo \"dingtalk:ACCESS_TOKEN:SECxxx\" > %s\n", keyPath)
}

func cmdUninstall(projectMode bool) {
	settingsPath := settingsFilePath(projectMode)
	if err := removeHooksFromSettings(settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Hooks removed from: %s\n", settingsPath)

	if !projectMode {
		home, _ := os.UserHomeDir()
		binPath := filepath.Join(home, installDir, binaryName)
		if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: could not remove binary: %v\n", err)
		} else if err == nil {
			fmt.Printf("✓ Binary removed: %s\n", binPath)
		}
	}

	fmt.Println("✓ claude-notify uninstalled successfully!")
}

func settingsFilePath(projectMode bool) string {
	if projectMode {
		return filepath.Join(".claude", "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func installBinary() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	destDir := filepath.Join(home, installDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", destDir, err)
	}

	srcPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	srcPath, err = filepath.EvalSymlinks(srcPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	destPath := filepath.Join(destDir, binaryName)
	if srcPath == destPath {
		fmt.Println("  Binary already at target location, skipped.")
		return nil
	}

	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("cannot read binary: %w", err)
	}
	if err := os.WriteFile(destPath, src, 0755); err != nil {
		return fmt.Errorf("cannot write binary: %w", err)
	}
	return nil
}

// hookEntryJSON is the raw JSON value for each hook event
const hookEntryJSON = `[{"hooks":[{"type":"command","command":"` + hookCommand + `"}]}]`

func addHooksToSettings(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
	}

	jsonStr := string(data)
	for _, event := range hookEvents {
		jsonStr, err = sjson.SetRaw(jsonStr, "hooks."+event, hookEntryJSON)
		if err != nil {
			return fmt.Errorf("cannot set hooks.%s: %w", event, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}

	// Pretty-print with 2-space indent to match existing style
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(jsonStr), "", "  "); err != nil {
		return fmt.Errorf("cannot format JSON: %w", err)
	}
	buf.WriteByte('\n')

	return os.WriteFile(path, buf.Bytes(), 0644)
}

func removeHooksFromSettings(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  Settings file not found, nothing to remove.")
			return nil
		}
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	jsonStr := string(data)
	for _, event := range hookEvents {
		jsonStr, _ = sjson.Delete(jsonStr, "hooks."+event)
	}

	// If hooks object is now empty, remove it entirely
	hooks := gjson.Get(jsonStr, "hooks")
	if hooks.Exists() && hooks.IsObject() && len(hooks.Map()) == 0 {
		jsonStr, _ = sjson.Delete(jsonStr, "hooks")
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(jsonStr), "", "  "); err != nil {
		return fmt.Errorf("cannot format JSON: %w", err)
	}
	buf.WriteByte('\n')

	return os.WriteFile(path, buf.Bytes(), 0644)
}
