package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Prompt is an MCP prompt exposed by a server. It surfaces in the chat TUI as a
// slash command "/mcp__<server>__<prompt>"; running it fetches the rendered
// prompt and sends it to the model as a turn.
type Prompt struct {
	Name        string      // "mcp__<server>__<prompt>" — the slash-command body
	Server      string      // owning server name
	Raw         string      // original prompt name for prompts/get
	Description string      // human-readable summary
	Args        []PromptArg // declared arguments, in order
	client      *Client
}

// PromptArg is one declared prompt argument. Reasonix maps space-separated
// positional command arguments onto these in order, matching Claude Code.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Get fetches the prompt with the given arguments and flattens its returned
// messages into a single text block to send to the model.
func (p Prompt) Get(ctx context.Context, args map[string]string) (string, error) {
	return p.client.getPrompt(ctx, p.Raw, args)
}

func (c *Client) listPrompts(ctx context.Context) ([]Prompt, error) {
	res, err := c.call(ctx, "prompts/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Prompts []struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Arguments   []PromptArg `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("plugin %q: decode prompts/list: %w", c.name, err)
	}
	prompts := make([]Prompt, 0, len(out.Prompts))
	for _, p := range out.Prompts {
		prompts = append(prompts, Prompt{
			Name:        "mcp__" + normalizeName(c.name) + "__" + normalizeName(p.Name),
			Server:      c.name,
			Raw:         p.Name,
			Description: p.Description,
			Args:        p.Arguments,
			client:      c,
		})
	}
	return prompts, nil
}

func (c *Client) getPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	res, err := c.call(ctx, "prompts/get", params)
	if err != nil {
		return "", err
	}
	var out struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("plugin %q: decode prompts/get: %w", c.name, err)
	}
	var sb strings.Builder
	for _, m := range out.Messages {
		if m.Content.Type == "text" && m.Content.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(m.Content.Text)
		}
	}
	return sb.String(), nil
}

// ── OPT-05: 工具 Schema 压缩 ──
// 压缩工具的 JSON Schema 定义，减少 token 占用。
// 策略：移除非必要字段（$schema/$id/title/examples）、压缩长描述。
// Anthropic Token-Efficient Tools 方案：工具定义 token 减少 14-70%。

// CompactToolSchema 压缩工具 schema 的 JSON 定义
func CompactToolSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw // 解析失败，返回原始
	}
	compactSchemaMap(schema)
	out, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return out
}

// compactSchemaMap 递归压缩 schema map
func compactSchemaMap(m map[string]any) {
	// 移除非必要字段
	delete(m, "$schema")
	delete(m, "$id")
	delete(m, "title")
	delete(m, "examples")
	delete(m, "$comment")

	// 压缩顶层 description
	if desc, ok := m["description"].(string); ok && len(desc) > 200 {
		m["description"] = compactDescription(desc)
	}

	// 递归处理 properties
	if props, ok := m["properties"].(map[string]any); ok {
		for _, val := range props {
			if propMap, ok := val.(map[string]any); ok {
				compactSchemaMap(propMap)
			}
		}
	}

	// 递归处理 items（数组类型）
	if items, ok := m["items"].(map[string]any); ok {
		compactSchemaMap(items)
	}

	// 递归处理 additionalProperties
	if addl, ok := m["additionalProperties"].(map[string]any); ok {
		compactSchemaMap(addl)
	}

	// 递归处理 allOf/anyOf/oneOf
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if arr, ok := m[key].([]any); ok {
			for _, item := range arr {
				if itemMap, ok := item.(map[string]any); ok {
					compactSchemaMap(itemMap)
				}
			}
		}
	}
}

// compactDescription 压缩描述文本：保留第一句话 + 关键约束
func compactDescription(desc string) string {
	// 保留第一句话
	for _, sep := range []string{". ", "。", "! ", "？", "? "} {
		idx := strings.Index(desc, sep)
		if idx > 0 && idx < 200 {
			return desc[:idx+len(sep)]
		}
	}
	// 保留前 180 字符
	if len(desc) > 180 {
		return desc[:177] + "..."
	}
	return desc
}

// ── OPT-08: 工具描述沙箱化 ──
// MCP 工具描述直接来自外部 Server，可能包含指令注入（工具投毒攻击）。
// 在工具注册时过滤描述中的潜在注入内容。

// instructionInjectionPatterns 指令注入检测模式（中英文）
var instructionInjectionPatterns = []string{
	"ignore previous",
	"forget your instructions",
	"you are now",
	"system:",
	"important: ignore",
	"disregard",
	"override your",
	"new instructions",
	"actually, you are",
	"忽略之前的",
	"你的新任务是",
	"系统指令",
	"实际上你是",
	"忘记你的指令",
	"重要：忽略",
}

// SanitizeToolDescription 清理工具描述中的潜在指令注入
// 返回清理后的描述；如果检测到注入，截断到注入点之前并添加警告
func SanitizeToolDescription(desc string) string {
	if desc == "" {
		return desc
	}
	desc = strings.TrimSpace(desc)
	// 限制长度
	if len(desc) > 2000 {
		desc = desc[:1997] + "..."
	}
	lower := strings.ToLower(desc)
	for _, pattern := range instructionInjectionPatterns {
		idx := strings.Index(lower, strings.ToLower(pattern))
		if idx >= 0 {
			if idx > 50 {
				return desc[:idx] + "\n[注意: 检测到潜在指令注入, 已截断]"
			}
			return "[注意: 工具描述包含潜在指令注入, 已移除]"
		}
	}
	return desc
}
