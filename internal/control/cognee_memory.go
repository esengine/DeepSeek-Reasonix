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

	"reasonix/internal/agent"
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

type cogneeSchema struct {
	queryField    string
	contentField  string
	categoryField string
	scopeField    string
	sourceField   string
	actionField   string
	actionValue   string
}

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
	if _, _, err := c.cogneeReadTool(ctx, spec); err != nil {
		status.State, status.Detail = "tools_incompatible", "The Cognee server does not provide compatible get_guidance or search_memory tools."
		return status
	}
	if _, err := c.cogneeTool(ctx, spec, "propose_memory"); err != nil {
		status.State, status.Detail = "tools_incompatible", "The Cognee server does not provide a compatible propose_memory tool."
		return status
	}
	status.State, status.Detail = "ready", "VISION-LIAO memory guidance and proposal tools are ready."
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

func (c *Controller) cogneeGuidanceContext(ctx context.Context, raw string) (string, string) {
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
	toolName, schema, err := c.cogneeReadTool(ctx, spec)
	if err != nil {
		return "", "Cognee 项目记忆已跳过：get_guidance/search_memory 工具不可用"
	}
	args := map[string]any{schema.queryField: query}
	encoded, _ := json.Marshal(args)
	result, err := c.mcp.callToolForSpec(ctx, spec, toolName, encoded)
	if err != nil {
		return "", "Cognee 项目记忆检索失败；任务会继续"
	}
	result = redactCogneeText(result, maxCogneeRecallBytes)
	if strings.TrimSpace(result) == "" {
		return "", "Cognee 未返回可安全使用的项目记忆"
	}
	return result, "已检索 VISION-LIAO 记忆，并作为不可信上下文提供给本轮任务"
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

func (c *Controller) cogneeReadTool(ctx context.Context, spec plugin.Spec) (string, cogneeSchema, error) {
	for _, name := range []string{"get_guidance", "search_memory"} {
		schema, err := c.cogneeTool(ctx, spec, name)
		if err == nil {
			return name, schema, nil
		}
	}
	return "", cogneeSchema{}, fmt.Errorf("Cognee guidance/search tools unavailable")
}

func validateCogneeSchema(rawSchema json.RawMessage, toolName string) (cogneeSchema, error) {
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(rawSchema, &root) != nil {
		return cogneeSchema{}, fmt.Errorf("Cognee schema is invalid")
	}
	stringField := func(name string) bool { return schemaAllowsType(root.Properties[name], "string") }
	if toolName == "get_guidance" || toolName == "search_memory" {
		for _, field := range []string{"task", "query"} {
			if stringField(field) {
				return cogneeSchema{queryField: field}, nil
			}
		}
		return cogneeSchema{}, fmt.Errorf("Cognee %s schema lacks task/query", toolName)
	}
	if toolName == "propose_memory" {
		contentField := firstStringField(root.Properties, "content", "text")
		categoryField := firstStringField(root.Properties, "category")
		scopeField := firstStringField(root.Properties, "scope", "target_scope")
		sourceField := firstStringField(root.Properties, "source", "source_description")
		if contentField == "" || categoryField == "" || scopeField == "" || sourceField == "" {
			return cogneeSchema{}, fmt.Errorf("Cognee propose_memory schema lacks content/category/scope/source")
		}
		if !schemaAllowsEnumValue(root.Properties[scopeField], "private") || !schemaAllowsEnumValue(root.Properties[scopeField], "global") {
			return cogneeSchema{}, fmt.Errorf("Cognee propose_memory schema does not support private and global scopes")
		}
		if !schemaAllowsEnumValue(root.Properties[categoryField], "experience") {
			return cogneeSchema{}, fmt.Errorf("Cognee propose_memory schema does not support experience category")
		}
		result := cogneeSchema{contentField: contentField, categoryField: categoryField, scopeField: scopeField, sourceField: sourceField}
		if actionField := firstStringField(root.Properties, "action"); actionField != "" {
			result.actionField = actionField
			for _, value := range []string{"new", "create", "add"} {
				if schemaAllowsEnumValue(root.Properties[actionField], value) {
					result.actionValue = value
					break
				}
			}
			if result.actionValue == "" {
				return cogneeSchema{}, fmt.Errorf("Cognee propose_memory schema has no supported create action")
			}
		}
		return result, nil
	}
	return cogneeSchema{}, fmt.Errorf("Cognee tool schema is incompatible")
}

func firstStringField(properties map[string]json.RawMessage, fields ...string) string {
	for _, field := range fields {
		if schemaAllowsType(properties[field], "string") {
			return field
		}
	}
	return ""
}

func schemaAllowsEnumValue(raw json.RawMessage, value string) bool {
	var schema struct {
		Enum  []string          `json:"enum"`
		AnyOf []json.RawMessage `json:"anyOf"`
		OneOf []json.RawMessage `json:"oneOf"`
	}
	if json.Unmarshal(raw, &schema) != nil {
		return false
	}
	if !schemaAllowsType(raw, "string") {
		return false
	}
	if len(schema.Enum) > 0 {
		for _, allowed := range schema.Enum {
			if allowed == value {
				return true
			}
		}
		return false
	}
	if len(schema.AnyOf)+len(schema.OneOf) > 0 {
		for _, option := range append(schema.AnyOf, schema.OneOf...) {
			if schemaAllowsEnumValue(option, value) {
				return true
			}
		}
		return false
	}
	if len(raw) == 0 {
		return true
	}
	return true
}

func schemaAllowsType(raw json.RawMessage, want string) bool {
	var schema struct {
		Type  json.RawMessage   `json:"type"`
		AnyOf []json.RawMessage `json:"anyOf"`
		OneOf []json.RawMessage `json:"oneOf"`
	}
	if json.Unmarshal(raw, &schema) != nil {
		return false
	}
	var single string
	if json.Unmarshal(schema.Type, &single) == nil && single == want {
		return true
	}
	var multiple []string
	if json.Unmarshal(schema.Type, &multiple) == nil {
		for _, value := range multiple {
			if value == want {
				return true
			}
		}
	}
	for _, option := range append(schema.AnyOf, schema.OneOf...) {
		if schemaAllowsType(option, want) {
			return true
		}
	}
	return false
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

func buildCogneeTaskRecord(task, outcome, workspace string, categories []string) string {
	task = redactCogneeText(redactCogneeWorkspacePath(task, workspace), 1024)
	outcome = redactCogneeText(redactCogneeWorkspacePath(outcome, workspace), maxCogneeRememberBytes)
	compactOutcome := strings.Trim(strings.ToLower(strings.Join(strings.Fields(outcome), "")), ".,!。！?？")
	if task == "" || outcome == "" || utf8.RuneCountInString(strings.Join(strings.Fields(task+outcome), "")) < 24 {
		return ""
	}
	switch compactOutcome {
	case "ok", "done", "完成", "好的", "收到":
		return ""
	}
	record := "Task: " + task + "\nOutcome: " + outcome
	if len(categories) > 0 {
		record += "\nEvidence: " + strings.Join(categories, ", ")
	}
	return redactCogneeText(record, maxCogneeRememberBytes)
}

func redactCogneeWorkspacePath(raw, workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return raw
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return raw
	}
	absolute = filepath.Clean(absolute)
	if absolute == string(filepath.Separator) || absolute == filepath.VolumeName(absolute)+string(filepath.Separator) {
		return raw
	}
	candidates := []string{absolute}
	if strings.HasPrefix(workspace, "/") && workspace != "/" {
		candidates = append(candidates, workspace)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil && resolved != absolute {
		candidates = append(candidates, filepath.Clean(resolved))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		raw = strings.ReplaceAll(raw, candidate, "[WORKSPACE_PATH]")
		if runtime.GOOS == "windows" {
			matcher := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(candidate))
			raw = matcher.ReplaceAllString(raw, "[WORKSPACE_PATH]")
		}
	}
	return raw
}

func cogneeMemoryEligibility(snapshot agent.CompletionSnapshot, record string) (session, graph bool) {
	todosReady := !snapshot.TodosWritten || snapshot.TodosComplete
	session = strings.TrimSpace(record) != "" && todosReady
	graph = session && (snapshot.HasVerifiedEvidence || snapshot.HasObservedMutation)
	return session, graph
}

func (c *Controller) cogneeProposePrivate(ctx context.Context, taskKey, record string) string {
	return c.cogneeProposeDestination(ctx, taskKey, "private", record)
}

func (c *Controller) cogneeProposeGlobal(ctx context.Context, taskKey, record string) string {
	return c.cogneeProposeDestination(ctx, taskKey, "global", record)
}

func (c *Controller) cogneeProposeDestination(ctx context.Context, taskKey, scope, record string) string {
	if !c.cogneeMemoryEnabled() {
		return ""
	}
	if strings.TrimSpace(record) == "" {
		return "VISION-LIAO 记忆提案未提交：没有可复用的任务摘要"
	}
	spec, err := c.cogneeSpecIfReady(ctx)
	if err != nil {
		return "VISION-LIAO 记忆提案未提交：服务未连接或未授权"
	}
	schema, err := c.cogneeTool(ctx, spec, "propose_memory")
	if err != nil {
		return "VISION-LIAO 记忆提案未提交：propose_memory 工具不可用"
	}
	if scope != "private" && scope != "global" {
		return "VISION-LIAO 记忆提案未提交：目标范围无效"
	}
	argsMap := map[string]any{
		schema.contentField:  redactCogneeText(record, maxCogneeRememberBytes),
		schema.categoryField: "experience",
		schema.scopeField:    scope,
		schema.sourceField:   "Reasonix completed task outcome",
	}
	if schema.actionField != "" {
		argsMap[schema.actionField] = schema.actionValue
	}
	args, _ := json.Marshal(argsMap)
	if err := createCogneeAttempt(c.sessionDir, taskKey, scope); err != nil {
		if os.IsExist(err) {
			return "VISION-LIAO " + cogneeDestinationName(scope) + "未重复提交：此任务已有尝试记录"
		}
		return "VISION-LIAO " + cogneeDestinationName(scope) + "未提交：无法持久化一次性尝试标记"
	}
	result, err := c.mcp.callToolForSpec(ctx, spec, "propose_memory", args)
	if err != nil {
		finishCogneeAttempt(c.sessionDir, taskKey, scope, "uncertain")
		return "VISION-LIAO " + cogneeDestinationName(scope) + "提交结果不确定；Reasonix 不会自动重试"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result)), "error:") {
		finishCogneeAttempt(c.sessionDir, taskKey, scope, "uncertain")
		return "VISION-LIAO " + cogneeDestinationName(scope) + "返回错误，提交结果不确定；Reasonix 不会自动重试"
	}
	finishCogneeAttempt(c.sessionDir, taskKey, scope, "submitted")
	if strings.Contains(strings.ToLower(result), "pending") {
		return "VISION-LIAO " + cogneeDestinationName(scope) + "已提交，待桌面端审核"
	}
	return "VISION-LIAO " + cogneeDestinationName(scope) + "调用已完成；请在桌面端审核队列确认状态"
}

func cogneeDestinationName(scope string) string {
	if scope == "private" {
		return "私有记忆提案"
	}
	return "全局记忆提案"
}

func createCogneeAttempt(sessionDir, taskKey, destination string) error {
	if strings.TrimSpace(sessionDir) == "" || strings.TrimSpace(taskKey) == "" {
		return fmt.Errorf("missing task identity")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(sessionDir) + "\x00" + taskKey + "\x00" + destination))
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

func finishCogneeAttempt(sessionDir, taskKey, destination, status string) {
	if status != "submitted" && status != "uncertain" {
		return
	}
	sum := sha256.Sum256([]byte(filepath.Clean(sessionDir) + "\x00" + taskKey + "\x00" + destination))
	path := filepath.Join(config.ReasonixHomeDir(), "cognee-memory", hex.EncodeToString(sum[:])+".json")
	_ = fileutil.AtomicWriteFile(path, []byte("{\"status\":\""+status+"\"}"), 0600)
}

func (c *Controller) cogneeNotice(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Code: event.NoticeCodeCogneeMemory, Level: event.LevelInfo, Text: text})
}
