package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/fileutil"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

const (
	cogneeServerName       = "cognee"
	cogneeTokenCredential  = "REASONIX_COGNEE_MCP_TOKEN"
	maxCogneeQueryBytes    = 1024
	maxCogneeRecallBytes   = 8 << 10
	maxCogneeRememberBytes = 3 << 10
)

var (
	credentialAssignment = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|authorization)\s*[:=]\s*[^\s,;]+`)
	knownSecretToken     = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{12,}|gh[pousr]_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,}|AIza[0-9A-Za-z_-]{30,})\b`)
	bearerCredential     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}`)
	privateKeyBlock      = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	windowsAbsolutePath  = regexp.MustCompile(`(?i)\b[A-Z]:\\(?:[^\s"'<>|]+\\?)*`)
	unixHomePath         = regexp.MustCompile(`/(?:Users|home|root)/[^\s"'<>]+`)
)

type cogneeSchema struct{ datasetField string }

// CogneeMemoryStatus is a safe, read-only summary for the desktop settings UI.
// It deliberately excludes the endpoint and all credential material.
type CogneeMemoryStatus struct {
	State           string `json:"state"`
	Detail          string `json:"detail"`
	SkillEnabled    bool   `json:"skillEnabled"`
	Configured      bool   `json:"configured"`
	TokenConfigured bool   `json:"tokenConfigured"`
}

func (c *Controller) CogneeMemoryStatus(ctx context.Context) CogneeMemoryStatus {
	status := CogneeMemoryStatus{State: "not_configured"}
	if c == nil {
		status.Detail = "No active session is available."
		return status
	}
	cfg, err := config.LoadForRootWithoutCredentialsReadOnly(c.workspaceRoot)
	if err != nil {
		status.State, status.Detail = "connection_failed", "Cognee settings could not be read."
		return status
	}
	status.SkillEnabled = !cfg.IsSkillDisabled("cognee-memory")
	status.TokenConfigured = config.CredentialStored(cogneeTokenCredential)
	var entry config.PluginEntry
	for _, candidate := range cfg.Plugins {
		if strings.EqualFold(strings.TrimSpace(candidate.Name), cogneeServerName) {
			entry, status.Configured = candidate, true
			break
		}
	}
	if !status.Configured {
		status.Detail = "Configure a Cognee MCP endpoint to get started."
		return status
	}
	if !status.SkillEnabled {
		status.State, status.Detail = "disabled", "Cognee project memory is turned off in the Skills list."
		return status
	}
	spec := plugin.ResolveStoredAuthorization(ctx, c.mcpSpec(entry))
	if !spec.ServerAuthorized() {
		status.State, status.Detail = "authorization_required", "Authorize the Cognee MCP server to continue."
		return status
	}
	if err := validateCogneeEndpoint(spec); err != nil {
		status.State, status.Detail = "connection_failed", redactCogneeText(err.Error(), 240)
		return status
	}
	host := c.mcp.hostRef()
	if host == nil || !host.HasClientForSpec(spec) {
		status.State, status.Detail = "connection_failed", "Cognee is configured but not connected to this session."
		return status
	}
	if _, err := host.ToolsForSpec(ctx, spec); err != nil {
		status.State, status.Detail = "connection_failed", "Cognee tools could not be loaded."
		return status
	}
	if _, err := c.cogneeTool(ctx, spec, "recall"); err != nil {
		status.State, status.Detail = "tools_incompatible", "The Cognee server does not provide a compatible recall tool."
		return status
	}
	if _, err := c.cogneeTool(ctx, spec, "remember"); err != nil {
		status.State, status.Detail = "tools_incompatible", "The Cognee server does not provide a compatible remember tool."
		return status
	}
	status.State, status.Detail = "ready", "Cognee is connected and its required tools are ready."
	return status
}

func (c *Controller) cogneeMemoryEnabled() bool {
	if c == nil {
		return false
	}
	cfg, err := config.LoadForRootWithoutCredentialsReadOnly(c.workspaceRoot)
	return err == nil && !cfg.IsSkillDisabled("cognee-memory")
}

func (c *Controller) cogneeSpecIfReady(ctx context.Context) (plugin.Spec, error) {
	cfg, err := config.LoadForRootWithoutCredentialsReadOnly(c.workspaceRoot)
	if err != nil {
		return plugin.Spec{}, fmt.Errorf("could not read Cognee configuration")
	}
	var entry config.PluginEntry
	found := false
	for _, candidate := range cfg.Plugins {
		if strings.EqualFold(strings.TrimSpace(candidate.Name), cogneeServerName) {
			entry, found = candidate, true
			break
		}
	}
	if !found {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP server is not configured")
	}
	if !strings.EqualFold(strings.TrimSpace(entry.Name), cogneeServerName) {
		return plugin.Spec{}, fmt.Errorf("configured server name must be cognee")
	}
	if !config.MCPServerEnabled(entry, c.workspaceRoot) {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP server is not enabled for this workspace")
	}
	spec := c.mcpSpec(entry)
	spec = plugin.ResolveStoredAuthorization(ctx, spec)
	if !spec.ServerAuthorized() {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP server is not authorized")
	}
	if err := validateCogneeEndpoint(spec); err != nil {
		return plugin.Spec{}, err
	}
	host := c.mcp.hostRef()
	if host == nil {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP runtime is unavailable")
	}
	if !host.HasClientForSpec(spec) {
		if _, err := c.ConnectMCPServer(entry); err != nil {
			return plugin.Spec{}, fmt.Errorf("could not connect Cognee MCP server: %w", err)
		}
		spec = plugin.ResolveStoredAuthorization(ctx, c.mcpSpec(entry))
		host = c.mcp.hostRef()
	}
	if host == nil || !host.HasClientForSpec(spec) {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP server did not connect to this runtime")
	}
	if _, err := host.ToolsForSpec(ctx, spec); err != nil {
		return plugin.Spec{}, fmt.Errorf("Cognee MCP server is unavailable")
	}
	return spec, nil
}

func validateCogneeEndpoint(spec plugin.Spec) error {
	typ := strings.ToLower(strings.TrimSpace(spec.Type))
	if typ != "http" && typ != "streamable-http" && typ != "sse" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(spec.URL))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("Cognee endpoint is invalid")
	}
	for key := range u.Query() {
		key = strings.ToLower(key)
		if strings.Contains(key, "token") || strings.Contains(key, "key") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "auth") || strings.Contains(key, "credential") {
			return fmt.Errorf("Cognee endpoint must not carry credentials in its URL")
		}
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("remote Cognee MCP endpoints require HTTPS; HTTP is allowed only for loopback")
}

// ValidateCogneeEndpointForSettings applies the same URL policy to the desktop
// setup form as the runtime readiness check.
func ValidateCogneeEndpointForSettings(spec plugin.Spec) error {
	return validateCogneeEndpoint(spec)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (c *Controller) cogneeRecallContext(ctx context.Context, raw, workspace string) (string, string) {
	if !c.cogneeMemoryEnabled() {
		return "", ""
	}
	spec, err := c.cogneeSpecIfReady(ctx)
	if err != nil {
		return "", "Cognee 项目记忆已跳过：服务未连接或未授权"
	}
	query := redactCogneeText(raw, maxCogneeQueryBytes)
	if query == "" {
		return "", "Cognee 项目记忆已跳过：请求中没有可安全检索的文本"
	}
	dataset := cogneeDatasetName(workspace)
	schema, err := c.cogneeTool(ctx, spec, "recall")
	if err != nil {
		return "", "Cognee 项目记忆已跳过：recall 工具不可用"
	}
	args := map[string]any{"query": query}
	if schema.datasetField == "dataset_name" {
		args["dataset_name"] = dataset
	} else {
		args["datasets"] = []string{dataset}
	}
	encoded, _ := json.Marshal(args)
	result, err := c.mcp.callToolForSpec(ctx, spec, "recall", encoded)
	if err != nil {
		return "", "Cognee 项目记忆检索失败；任务会继续"
	}
	result = redactCogneeText(result, maxCogneeRecallBytes)
	if strings.TrimSpace(result) == "" {
		return "", "Cognee 未返回可安全使用的项目记忆"
	}
	return result, "已检索 Cognee 项目记忆，并作为不可信上下文提供给本轮任务"
}

func (c *Controller) cogneeTool(ctx context.Context, spec plugin.Spec, raw string) (cogneeSchema, error) {
	host := c.mcp.hostRef()
	tools, err := host.ToolsForSpec(ctx, spec)
	if err != nil {
		return cogneeSchema{}, err
	}
	for _, candidate := range tools {
		meta, ok := candidate.(tool.MCPMetadata)
		if !ok || meta.MCPRawToolName() != raw {
			continue
		}
		schema, err := validateCogneeSchema(candidate.Schema(), raw)
		if err != nil {
			return cogneeSchema{}, err
		}
		return schema, nil
	}
	return cogneeSchema{}, fmt.Errorf("Cognee %s tool unavailable", raw)
}

func validateCogneeSchema(rawSchema json.RawMessage, toolName string) (cogneeSchema, error) {
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(rawSchema, &root) != nil {
		return cogneeSchema{}, fmt.Errorf("Cognee schema is invalid")
	}
	stringField := func(name string) bool {
		var p struct {
			Type string `json:"type"`
		}
		return json.Unmarshal(root.Properties[name], &p) == nil && p.Type == "string"
	}
	if toolName == "recall" {
		if !stringField("query") {
			return cogneeSchema{}, fmt.Errorf("Cognee recall schema lacks query")
		}
		if stringField("dataset_name") {
			return cogneeSchema{datasetField: "dataset_name"}, nil
		}
		var datasets struct {
			Type  string `json:"type"`
			Items struct {
				Type string `json:"type"`
			} `json:"items"`
		}
		if json.Unmarshal(root.Properties["datasets"], &datasets) == nil && datasets.Type == "array" && datasets.Items.Type == "string" {
			return cogneeSchema{datasetField: "datasets"}, nil
		}
		return cogneeSchema{}, fmt.Errorf("Cognee recall schema lacks a dataset selector")
	}
	if toolName == "remember" && stringField("data") && stringField("dataset_name") {
		return cogneeSchema{datasetField: "dataset_name"}, nil
	}
	return cogneeSchema{}, fmt.Errorf("Cognee remember schema is incompatible")
}

func cogneeDatasetName(workspace string) string {
	canonical, err := filepath.Abs(workspace)
	if err != nil {
		canonical = filepath.Clean(workspace)
	}
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	identity := filepath.Clean(canonical)
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	sum := sha256.Sum256([]byte(identity))
	return "reasonix-" + hex.EncodeToString(sum[:16])
}

func redactCogneeText(raw string, limit int) string {
	text := strings.ToValidUTF8(raw, "")
	text = credentialAssignment.ReplaceAllString(text, "$1=[REDACTED]")
	text = knownSecretToken.ReplaceAllString(text, "[REDACTED]")
	text = bearerCredential.ReplaceAllString(text, "Bearer [REDACTED]")
	text = privateKeyBlock.ReplaceAllString(text, "[PRIVATE_KEY_REDACTED]")
	text = windowsAbsolutePath.ReplaceAllString(text, "[WORKSPACE_PATH]")
	text = unixHomePath.ReplaceAllString(text, "[WORKSPACE_PATH]")
	text = strings.TrimSpace(text)
	if len(text) > limit {
		text = text[:limit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}

func (c *Controller) cogneeRemember(ctx context.Context, taskKey, workspace, summary string, categories []string) string {
	if !c.cogneeMemoryEnabled() {
		return ""
	}
	if len(categories) == 0 || strings.TrimSpace(summary) == "" {
		return "Cognee 项目记忆未保存：缺少已核实的摘要"
	}
	spec, err := c.cogneeSpecIfReady(ctx)
	if err != nil {
		return "Cognee 项目记忆未保存：服务未连接或未授权"
	}
	dataset := cogneeDatasetName(workspace)
	_, err = c.cogneeTool(ctx, spec, "remember")
	if err != nil {
		return "Cognee 项目记忆未保存：remember 工具不可用"
	}
	data := "Verified project completion. Evidence: " + strings.Join(categories, ", ") + ". Summary: " + redactCogneeText(summary, maxCogneeRememberBytes)
	args, _ := json.Marshal(map[string]any{"data": data, "dataset_name": dataset})
	if err := createCogneeAttempt(c.sessionDir, taskKey); err != nil {
		if os.IsExist(err) {
			return "Cognee 项目记忆未重复保存：此任务已有持久化尝试记录"
		}
		return "Cognee 项目记忆未保存：无法持久化一次性尝试标记"
	}
	if _, err := c.mcp.callToolForSpec(ctx, spec, "remember", args); err != nil {
		finishCogneeAttempt(c.sessionDir, taskKey, "uncertain")
		return "Cognee 项目记忆保存结果不确定；Reasonix 不会自动重试"
	}
	finishCogneeAttempt(c.sessionDir, taskKey, "saved")
	return "已将经过核验的项目摘要保存到 Cognee"
}

func createCogneeAttempt(sessionDir, taskKey string) error {
	if strings.TrimSpace(sessionDir) == "" || strings.TrimSpace(taskKey) == "" {
		return fmt.Errorf("missing task identity")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(sessionDir) + "\x00" + taskKey))
	path := filepath.Join(config.ReasonixHomeDir(), "cognee-memory", hex.EncodeToString(sum[:])+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("{\"status\":\"pending\"}")
	if err == nil {
		err = f.Sync()
	}
	return err
}

func finishCogneeAttempt(sessionDir, taskKey, status string) {
	if status != "saved" && status != "uncertain" {
		return
	}
	sum := sha256.Sum256([]byte(filepath.Clean(sessionDir) + "\x00" + taskKey))
	path := filepath.Join(config.ReasonixHomeDir(), "cognee-memory", hex.EncodeToString(sum[:])+".json")
	_ = fileutil.AtomicWriteFile(path, []byte("{\"status\":\""+status+"\"}"), 0600)
}

func (c *Controller) cogneeNotice(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Code: event.NoticeCodeCogneeMemory, Level: event.LevelInfo, Text: text})
}
