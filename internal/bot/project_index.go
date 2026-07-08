package bot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/secrets"
)

const (
	botProjectListLimit = 20
	botSessionListLimit = 20
	botSearchListLimit  = 20
)

type botProjectEntry struct {
	ID      string
	Name    string
	Root    string
	Sources []string
}

type botSessionEntry struct {
	ID             string
	ProjectID      string
	ProjectName    string
	WorkspaceRoot  string
	SessionID      string
	SessionPath    string
	RemoteID       string
	ConnectionID   string
	ChatType       string
	UserID         string
	ThreadID       string
	Scope          string
	Preview        string
	TopicTitle     string
	LastActivityAt time.Time
	Source         string
}

type botProjectSearchResult struct {
	ProjectID   string
	ProjectName string
	Path        string
	Line        int
	Text        string
}

func (gw *BotGateway) buildProjectIndex() []botProjectEntry {
	collector := newBotProjectCollector()
	collector.add(gw.cfg.WorkspaceRoot, "default")

	// cfg.Channels / cfg.ConnectionChannels are rewritten under gw.mu at runtime,
	// so the whole scan shares the controllers critical section below.
	gw.mu.Lock()
	platforms := make([]string, 0, len(gw.cfg.Channels))
	for platform := range gw.cfg.Channels {
		platforms = append(platforms, string(platform))
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		channel := gw.cfg.Channels[Platform(platform)]
		source := "channel:" + platform
		collector.add(channel.WorkspaceRoot, source)
		addMappingProjects(collector, channel, source)
	}

	connections := make([]string, 0, len(gw.cfg.ConnectionChannels))
	for id := range gw.cfg.ConnectionChannels {
		connections = append(connections, id)
	}
	sort.Strings(connections)
	for _, id := range connections {
		channel := gw.cfg.ConnectionChannels[id]
		source := "connection:" + id
		collector.add(channel.WorkspaceRoot, source)
		addMappingProjects(collector, channel, source)
	}

	for i, route := range gw.cfg.Routes {
		collector.add(route.Channel.WorkspaceRoot, fmt.Sprintf("route:%d", i+1))
	}

	for key, state := range gw.controllers {
		root := ""
		if state != nil {
			root = state.workspaceRoot
			if root == "" && state.ctrl != nil {
				root = state.ctrl.WorkspaceRoot()
			}
		}
		collector.add(root, "active:"+shortBotID(key))
	}
	for key, override := range gw.sessionOverrides {
		collector.add(override.channel.WorkspaceRoot, "override:"+shortBotID(key))
	}
	gw.mu.Unlock()

	return collector.entries()
}

func addMappingProjects(collector *botProjectCollector, channel ChannelConfig, source string) {
	for _, mapping := range channel.SessionMappings {
		root := workspaceRootForSessionMapping(mapping, channel.WorkspaceRoot)
		collector.add(root, source+":mapping:"+strings.TrimSpace(mapping.RemoteID))
	}
}

type botProjectCollector struct {
	byRoot map[string]*botProjectEntry
}

func newBotProjectCollector() *botProjectCollector {
	return &botProjectCollector{byRoot: make(map[string]*botProjectEntry)}
}

func (c *botProjectCollector) add(root, source string) {
	root = canonicalBotPath(root)
	if root == "" {
		return
	}
	entry := c.byRoot[root]
	if entry == nil {
		entry = &botProjectEntry{Name: botProjectName(root), Root: root}
		c.byRoot[root] = entry
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	for _, existing := range entry.Sources {
		if existing == source {
			return
		}
	}
	entry.Sources = append(entry.Sources, source)
}

func (c *botProjectCollector) entries() []botProjectEntry {
	out := make([]botProjectEntry, 0, len(c.byRoot))
	for _, entry := range c.byRoot {
		copied := *entry
		sort.Strings(copied.Sources)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool {
		li := strings.ToLower(out[i].Name)
		lj := strings.ToLower(out[j].Name)
		if li != lj {
			return li < lj
		}
		return out[i].Root < out[j].Root
	})
	for i := range out {
		out[i].ID = fmt.Sprintf("p%d", i+1)
	}
	return out
}

func (gw *BotGateway) buildSessionIndex(projects []botProjectEntry) []botSessionEntry {
	projectByRoot := make(map[string]botProjectEntry, len(projects))
	for _, project := range projects {
		projectByRoot[canonicalBotPath(project.Root)] = project
	}
	collector := newBotSessionCollector(projectByRoot)

	// cfg.Channels / cfg.ConnectionChannels are rewritten under gw.mu at runtime;
	// collect the mapping-derived entries under a short lock, keeping the
	// filesystem scan below outside it.
	gw.mu.Lock()
	platforms := make([]string, 0, len(gw.cfg.Channels))
	for platform := range gw.cfg.Channels {
		platforms = append(platforms, string(platform))
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		channel := gw.cfg.Channels[Platform(platform)]
		addMappingSessions(collector, channel, "", "channel:"+platform)
	}

	connections := make([]string, 0, len(gw.cfg.ConnectionChannels))
	for id := range gw.cfg.ConnectionChannels {
		connections = append(connections, id)
	}
	sort.Strings(connections)
	for _, id := range connections {
		channel := gw.cfg.ConnectionChannels[id]
		addMappingSessions(collector, channel, id, "connection:"+id)
	}
	gw.mu.Unlock()

	for _, project := range projects {
		dir := botSessionDir(project.Root)
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		infos, err := agent.ListSessions(dir)
		if err != nil {
			gw.logger.Warn("bot project session index failed", "project", project.Name, "err", err)
			continue
		}
		for _, info := range infos {
			collector.add(botSessionEntry{
				WorkspaceRoot:  project.Root,
				SessionPath:    canonicalBotPath(info.Path),
				SessionID:      botSessionTarget(info.Path),
				Scope:          firstNonEmptyString(info.Scope, "project"),
				Preview:        info.Preview,
				TopicTitle:     info.TopicTitle,
				LastActivityAt: info.LastActivityAt,
				Source:         "project-sessions",
			})
		}
	}

	return collector.entries()
}

func addMappingSessions(collector *botSessionCollector, channel ChannelConfig, connectionID, source string) {
	for _, mapping := range channel.SessionMappings {
		sessionID := strings.TrimSpace(mapping.SessionID)
		sessionPath := botSessionPathFromTarget(sessionID)
		root := workspaceRootForSessionMapping(mapping, channel.WorkspaceRoot)
		collector.add(botSessionEntry{
			WorkspaceRoot:  root,
			SessionID:      sessionID,
			SessionPath:    sessionPath,
			RemoteID:       strings.TrimSpace(mapping.RemoteID),
			ConnectionID:   strings.TrimSpace(connectionID),
			ChatType:       strings.TrimSpace(mapping.ChatType),
			UserID:         strings.TrimSpace(mapping.UserID),
			ThreadID:       strings.TrimSpace(mapping.ThreadID),
			Scope:          strings.TrimSpace(mapping.Scope),
			LastActivityAt: parseBotMappingUpdatedAt(mapping.UpdatedAt),
			Source:         source,
		})
	}
}

type botSessionCollector struct {
	projectByRoot map[string]botProjectEntry
	byKey         map[string]*botSessionEntry
}

func newBotSessionCollector(projectByRoot map[string]botProjectEntry) *botSessionCollector {
	return &botSessionCollector{
		projectByRoot: projectByRoot,
		byKey:         make(map[string]*botSessionEntry),
	}
}

func (c *botSessionCollector) add(entry botSessionEntry) {
	entry.WorkspaceRoot = canonicalBotPath(entry.WorkspaceRoot)
	entry.SessionPath = canonicalBotPath(entry.SessionPath)
	if entry.SessionID == "" && entry.SessionPath != "" {
		entry.SessionID = botSessionTarget(entry.SessionPath)
	}
	if entry.SessionPath == "" && entry.SessionID == "" && entry.RemoteID == "" {
		return
	}
	if project, ok := c.projectByRoot[entry.WorkspaceRoot]; ok {
		entry.ProjectID = project.ID
		entry.ProjectName = project.Name
	}
	key := entry.SessionPath
	if key == "" {
		key = strings.Join([]string{"target", entry.ConnectionID, entry.RemoteID, entry.ChatType, entry.UserID, entry.ThreadID, entry.SessionID}, "\x00")
	}
	existing := c.byKey[key]
	if existing == nil {
		c.byKey[key] = &entry
		return
	}
	mergeBotSessionEntry(existing, entry)
}

func mergeBotSessionEntry(dst *botSessionEntry, src botSessionEntry) {
	if dst.ProjectID == "" {
		dst.ProjectID = src.ProjectID
	}
	if dst.ProjectName == "" {
		dst.ProjectName = src.ProjectName
	}
	if dst.WorkspaceRoot == "" {
		dst.WorkspaceRoot = src.WorkspaceRoot
	}
	if dst.SessionID == "" {
		dst.SessionID = src.SessionID
	}
	if dst.SessionPath == "" {
		dst.SessionPath = src.SessionPath
	}
	if dst.RemoteID == "" {
		dst.RemoteID = src.RemoteID
	}
	if dst.ConnectionID == "" {
		dst.ConnectionID = src.ConnectionID
	}
	if dst.ChatType == "" {
		dst.ChatType = src.ChatType
	}
	if dst.UserID == "" {
		dst.UserID = src.UserID
	}
	if dst.ThreadID == "" {
		dst.ThreadID = src.ThreadID
	}
	if dst.Scope == "" {
		dst.Scope = src.Scope
	}
	if dst.Preview == "" {
		dst.Preview = src.Preview
	}
	if dst.TopicTitle == "" {
		dst.TopicTitle = src.TopicTitle
	}
	if src.LastActivityAt.After(dst.LastActivityAt) {
		dst.LastActivityAt = src.LastActivityAt
	}
	if dst.Source == "" {
		dst.Source = src.Source
	} else if src.Source != "" && !strings.Contains(dst.Source, src.Source) {
		dst.Source += "," + src.Source
	}
}

func (c *botSessionCollector) entries() []botSessionEntry {
	out := make([]botSessionEntry, 0, len(c.byKey))
	for _, entry := range c.byKey {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		}
		if out[i].ProjectName != out[j].ProjectName {
			return out[i].ProjectName < out[j].ProjectName
		}
		return out[i].SessionPath < out[j].SessionPath
	})
	for i := range out {
		out[i].ID = fmt.Sprintf("s%d", i+1)
	}
	return out
}

func botSessionPathFromTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "path:") {
		return canonicalBotPath(strings.TrimPrefix(target, "path:"))
	}
	if filepath.IsAbs(target) && strings.HasSuffix(target, ".jsonl") {
		return canonicalBotPath(target)
	}
	return ""
}

func parseBotMappingUpdatedAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func resolveBotProject(projects []botProjectEntry, selector string) (botProjectEntry, []botProjectEntry) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return botProjectEntry{}, nil
	}
	selectorLower := strings.ToLower(selector)
	canonicalSelector := canonicalBotPath(selector)
	var matches []botProjectEntry
	for _, project := range projects {
		if strings.EqualFold(project.ID, selector) || canonicalBotPath(project.Root) == canonicalSelector || strings.EqualFold(project.Name, selector) {
			return project, nil
		}
		if strings.Contains(strings.ToLower(project.Name), selectorLower) || strings.Contains(strings.ToLower(project.Root), selectorLower) {
			matches = append(matches, project)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return botProjectEntry{}, matches
}

func resolveBotSession(sessions []botSessionEntry, selector string) (botSessionEntry, []botSessionEntry) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return botSessionEntry{}, nil
	}
	selectorLower := strings.ToLower(selector)
	canonicalSelector := canonicalBotPath(selector)
	var matches []botSessionEntry
	for _, session := range sessions {
		if strings.EqualFold(session.ID, selector) ||
			(session.SessionPath != "" && canonicalBotPath(session.SessionPath) == canonicalSelector) ||
			(session.SessionPath != "" && strings.EqualFold(filepath.Base(session.SessionPath), selector)) ||
			(session.SessionID != "" && strings.EqualFold(session.SessionID, selector)) {
			return session, nil
		}
		if botSessionMatchesQuery(session, selectorLower) {
			matches = append(matches, session)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return botSessionEntry{}, matches
}

func filterBotProjects(projects []botProjectEntry, query string) []botProjectEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return projects
	}
	var out []botProjectEntry
	for _, project := range projects {
		if strings.Contains(strings.ToLower(project.Name+" "+project.Root+" "+strings.Join(project.Sources, " ")), query) {
			out = append(out, project)
		}
	}
	return out
}

func filterBotSessions(sessions []botSessionEntry, query string) []botSessionEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return sessions
	}
	var out []botSessionEntry
	for _, session := range sessions {
		if botSessionMatchesQuery(session, query) {
			out = append(out, session)
		}
	}
	return out
}

func botSessionMatchesQuery(session botSessionEntry, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		session.ID,
		session.ProjectID,
		session.ProjectName,
		session.WorkspaceRoot,
		session.SessionID,
		session.SessionPath,
		session.RemoteID,
		session.ConnectionID,
		session.ChatType,
		session.UserID,
		session.ThreadID,
		session.Scope,
		session.Preview,
		session.TopicTitle,
		session.Source,
	}, " "))
	return strings.Contains(haystack, query)
}

func formatBotProjects(projects []botProjectEntry, query string, limit int) string {
	matches := filterBotProjects(projects, query)
	if len(matches) == 0 {
		if strings.TrimSpace(query) == "" {
			return "还没有可用项目索引。请先在 bot 连接、route 或当前会话里配置 workspace_root。"
		}
		return "没有匹配的项目。"
	}
	if limit <= 0 || limit > len(matches) {
		limit = len(matches)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "项目索引（%d/%d）：", limit, len(matches))
	for i := 0; i < limit; i++ {
		project := matches[i]
		fmt.Fprintf(&b, "\n%s %s — %s", project.ID, project.Name, displayBotPath(project.Root))
		if len(project.Sources) > 0 {
			fmt.Fprintf(&b, "\n  来源: %s", strings.Join(project.Sources, ", "))
		}
	}
	if len(matches) > limit {
		fmt.Fprintf(&b, "\n还有 %d 个结果，请加关键词缩小范围。", len(matches)-limit)
	}
	return b.String()
}

func formatBotSessions(sessions []botSessionEntry, query string, limit int) string {
	matches := filterBotSessions(sessions, query)
	if len(matches) == 0 {
		if strings.TrimSpace(query) == "" {
			return "还没有可用会话索引。已有项目会话或 bot session_mappings 后会出现在这里。"
		}
		return "没有匹配的会话。"
	}
	if limit <= 0 || limit > len(matches) {
		limit = len(matches)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "会话索引（%d/%d）：", limit, len(matches))
	for i := 0; i < limit; i++ {
		session := matches[i]
		project := firstNonEmptyString(session.ProjectName, "global")
		fmt.Fprintf(&b, "\n%s %s", session.ID, project)
		if session.TopicTitle != "" {
			fmt.Fprintf(&b, " · %s", singleLineBotText(session.TopicTitle, 40))
		}
		if session.Preview != "" {
			fmt.Fprintf(&b, "\n  预览: %s", singleLineBotText(session.Preview, 90))
		}
		if session.SessionPath != "" {
			fmt.Fprintf(&b, "\n  文件: %s", displayBotPath(session.SessionPath))
		} else if session.SessionID != "" {
			fmt.Fprintf(&b, "\n  目标: %s", session.SessionID)
		}
		if session.RemoteID != "" || session.ConnectionID != "" {
			fmt.Fprintf(&b, "\n  远端: %s %s", session.ConnectionID, session.RemoteID)
		}
	}
	if len(matches) > limit {
		fmt.Fprintf(&b, "\n还有 %d 个结果，请加关键词缩小范围。", len(matches)-limit)
	}
	return b.String()
}

func formatBotProjectSearchResults(results []botProjectSearchResult, limit int) string {
	if len(results) == 0 {
		return "没有跨项目命中。"
	}
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "跨项目检索结果（%d/%d）：", limit, len(results))
	for i := 0; i < limit; i++ {
		result := results[i]
		project := firstNonEmptyString(result.ProjectName, result.ProjectID)
		fmt.Fprintf(&b, "\n- %s %s:%d: %s", project, displayBotPath(result.Path), result.Line, singleLineBotText(result.Text, 120))
	}
	if len(results) > limit {
		fmt.Fprintf(&b, "\n还有 %d 条命中，请加关键词缩小范围。", len(results)-limit)
	}
	return b.String()
}

func searchBotProjects(ctx context.Context, projects []botProjectEntry, query string, limit int) ([]botProjectSearchResult, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, errors.New("检索词至少需要 2 个字符")
	}
	var roots []string
	seen := map[string]bool{}
	for _, project := range projects {
		root := canonicalBotPath(project.Root)
		if root == "" || seen[root] {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	if len(roots) == 0 {
		return nil, errors.New("没有可检索的项目目录")
	}
	if limit <= 0 {
		limit = botSearchListLimit
	}
	if rg, err := exec.LookPath("rg"); err == nil {
		return searchBotProjectsWithRG(ctx, rg, projects, roots, query, limit)
	}
	return searchBotProjectsFallback(ctx, projects, roots, query, limit)
}

func searchBotProjectsWithRG(ctx context.Context, rg string, projects []botProjectEntry, roots []string, query string, limit int) ([]botProjectSearchResult, error) {
	args := []string{
		"--json",
		"--color", "never",
		"--fixed-strings",
		"--max-count", "3",
		"--max-filesize", "1M",
		"--glob", "!.git",
		"--glob", "!node_modules",
		"--glob", "!dist",
		"--glob", "!build",
		"--glob", "!vendor",
		"--",
		query,
	}
	args = append(args, roots...)
	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Env = secrets.ProcessEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("rg failed: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var results []botProjectSearchResult
	for {
		var item struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				Lines struct {
					Text string `json:"text"`
				} `json:"lines"`
				LineNumber int `json:"line_number"`
			} `json:"data"`
		}
		if err := dec.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			break
		}
		if item.Type != "match" {
			continue
		}
		path := canonicalBotPath(item.Data.Path.Text)
		project := botProjectForPath(projects, path)
		results = append(results, botProjectSearchResult{
			ProjectID:   project.ID,
			ProjectName: project.Name,
			Path:        path,
			Line:        item.Data.LineNumber,
			Text:        strings.TrimSpace(item.Data.Lines.Text),
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

var errStopBotSearch = errors.New("stop bot project search")

func searchBotProjectsFallback(ctx context.Context, projects []botProjectEntry, roots []string, query string, limit int) ([]botProjectSearchResult, error) {
	queryLower := strings.ToLower(query)
	var results []botProjectSearchResult
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if shouldSkipBotSearchDir(d.Name()) && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > 1024*1024 {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			line := 0
			for scanner.Scan() {
				if err := ctx.Err(); err != nil {
					_ = file.Close()
					return err
				}
				line++
				text := scanner.Text()
				if strings.Contains(strings.ToLower(text), queryLower) {
					project := botProjectForPath(projects, path)
					results = append(results, botProjectSearchResult{
						ProjectID:   project.ID,
						ProjectName: project.Name,
						Path:        canonicalBotPath(path),
						Line:        line,
						Text:        strings.TrimSpace(text),
					})
					if len(results) >= limit {
						_ = file.Close()
						return errStopBotSearch
					}
				}
			}
			_ = file.Close()
			return nil
		})
		if errors.Is(walkErr, errStopBotSearch) {
			break
		}
		if walkErr != nil {
			return results, walkErr
		}
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func shouldSkipBotSearchDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "vendor", ".next", ".cache":
		return true
	default:
		return false
	}
}

func botProjectForPath(projects []botProjectEntry, path string) botProjectEntry {
	path = canonicalBotPath(path)
	var best botProjectEntry
	for _, project := range projects {
		root := canonicalBotPath(project.Root)
		if root == "" {
			continue
		}
		if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			if len(root) > len(best.Root) {
				best = project
			}
		}
	}
	return best
}

func canonicalBotPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func botProjectName(root string) string {
	root = strings.TrimRight(canonicalBotPath(root), string(os.PathSeparator))
	if root == "" {
		return ""
	}
	name := filepath.Base(root)
	if name == "." || name == string(os.PathSeparator) {
		return root
	}
	return name
}

func displayBotPath(path string) string {
	path = canonicalBotPath(path)
	home, err := os.UserHomeDir()
	if err == nil {
		home = canonicalBotPath(home)
		if home != "" && (path == home || strings.HasPrefix(path, home+string(os.PathSeparator))) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func singleLineBotText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

func shortBotID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
