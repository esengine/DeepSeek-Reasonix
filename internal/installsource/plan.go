package installsource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/skill"
)

var githubAPIBaseURL = "https://api.github.com"

// plan turns a request into a list of actions plus a warnings slice. It
// does not touch the disk; the apply phase is responsible for side effects.
func (t *installSourceTool) plan(ctx context.Context, req request) ([]action, []string, error) {
	if isURL(req.Source) {
		return t.planURL(ctx, req)
	}
	path := t.resolvePath(req.Source)
	if info, err := os.Stat(path); err == nil {
		return t.planLocal(req, path, info)
	}
	if req.Kind == "auto" || req.Kind == "mcp" {
		if looksLikePackage(req.Source) {
			return []action{t.packageMCPAction(req)}, nil, nil
		}
	}
	return nil, nil, newErr(ErrSourceUnreadable, "source %q is not a readable local path, URL, or supported package name", req.Source)
}

func (t *installSourceTool) planURL(ctx context.Context, req request) ([]action, []string, error) {
	rawURL := rawGitHubBlobURL(req.Source)
	if req.Kind == "mcp" && !looksLikeMarkdownURL(rawURL) && !looksLikeMCPJSONURL(rawURL) {
		return []action{t.remoteMCPAction(req, rawURL)}, nil, nil
	}
	if looksLikeMarkdownURL(rawURL) || looksLikeMCPJSONURL(rawURL) || rawURL != req.Source {
		actions, warnings, err := t.planDownloadedURL(ctx, req, rawURL)
		if err == nil && len(actions) > 0 {
			return actions, warnings, nil
		}
		if req.Kind != "auto" {
			return nil, warnings, err
		}
	}
	if gitActions, warnings := t.tryGitHubRepo(ctx, req); len(gitActions) > 0 {
		return gitActions, warnings, nil
	}
	if req.Kind == "skill" {
		return nil, nil, newErr(ErrUnsupportedKind, "URL %q is not a direct markdown skill file or GitHub SKILL.md", req.Source)
	}
	if req.Kind == "auto" && !looksLikeRemoteMCPEndpoint(req.Source) {
		return nil, nil, newErr(ErrUnsupportedKind, "URL %q is not a direct MCP endpoint or skill manifest; provide a raw SKILL.md, .mcp.json, or use kind='mcp' for a remote MCP endpoint", req.Source)
	}
	return []action{t.remoteMCPAction(req, req.Source)}, nil, nil
}

func (t *installSourceTool) planDownloadedURL(ctx context.Context, req request, sourceURL string) ([]action, []string, error) {
	body, err := t.fetchText(ctx, sourceURL)
	if err != nil {
		return nil, nil, err
	}
	if req.Kind == "auto" || req.Kind == "mcp" {
		entries, warnings, err := parseMCPJSON([]byte(body))
		if err == nil && len(entries) > 0 {
			actions := make([]action, 0, len(entries))
			for _, e := range entries {
				actions = append(actions, t.mcpEntryAction(req, e, sourceURL))
			}
			return actions, warnings, nil
		}
	}
	if req.Kind == "auto" || req.Kind == "skill" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = nameFromURL(sourceURL)
		}
		cand, err := parseSkillContent(body, name, sourceURL, req.strict())
		if err == nil {
			return []action{t.skillAction(req, cand, "copy")}, nil, nil
		}
		return nil, nil, err
	}
	return nil, nil, newErr(ErrUnsupportedKind, "downloaded URL did not contain a requested %s install source", req.Kind)
}

func (t *installSourceTool) tryGitHubRepo(ctx context.Context, req request) ([]action, []string) {
	if req.Kind != "auto" && req.Kind != "skill" && req.Kind != "mcp" {
		return nil, nil
	}
	src, ok := parseGitHubRepoSource(req.Source)
	if !ok {
		return nil, nil
	}
	var warnings []string
	for _, branch := range src.branches() {
		if req.Kind == "auto" || req.Kind == "mcp" {
			cand := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", src.Owner, src.Repo, branch, joinURLPath(src.Path, ".mcp.json"))
			actions, _, err := t.planDownloadedURL(ctx, req, cand)
			if err == nil && len(actions) > 0 {
				return actions, warnings
			}
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s", cand, err.Error()))
			}
		}
		if req.Kind == "auto" || req.Kind == "skill" {
			actions, skillWarnings, err := t.planGitHubSkillRepo(ctx, req, src, branch)
			warnings = append(warnings, skillWarnings...)
			if err == nil && len(actions) > 0 {
				return actions, warnings
			}
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("github repo %s/%s@%s: %s", src.Owner, src.Repo, branch, err.Error()))
			}
		}
	}
	return nil, warnings
}

type githubRepoSource struct {
	Owner  string
	Repo   string
	Branch string
	Path   string
}

func (s githubRepoSource) branches() []string {
	if s.Branch != "" {
		return []string{s.Branch}
	}
	return []string{"main", "master"}
}

func parseGitHubRepoSource(source string) (githubRepoSource, bool) {
	u, err := url.Parse(source)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return githubRepoSource{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return githubRepoSource{}, false
	}
	out := githubRepoSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) >= 4 && parts[2] == "tree" {
		out.Branch = parts[3]
		if len(parts) > 4 {
			out.Path = strings.Join(parts[4:], "/")
		}
	}
	return out, true
}

type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

func (t *installSourceTool) planGitHubSkillRepo(ctx context.Context, req request, src githubRepoSource, branch string) ([]action, []string, error) {
	cands, warnings, err := t.scanGitHubSkills(ctx, req, src, branch)
	if err != nil {
		return nil, warnings, err
	}
	if len(cands) == 0 {
		return nil, warnings, newErr(ErrManifestMissing, "no SKILL.md or <name>.md skills found under GitHub repo path %s", firstNonEmpty(src.Path, "."))
	}
	actions := make([]action, 0, len(cands))
	for _, cand := range cands {
		actions = append(actions, t.skillAction(req, cand, "copy"))
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })
	return actions, warnings, nil
}

func (t *installSourceTool) scanGitHubSkills(ctx context.Context, req request, src githubRepoSource, branch string) ([]skillCandidate, []string, error) {
	var out []skillCandidate
	var warnings []string
	var walk func(path string, depth int) error
	walk = func(path string, depth int) error {
		if depth > maxSkillScanDepth {
			return nil
		}
		entries, err := t.fetchGitHubContents(ctx, src, branch, path)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		for _, entry := range entries {
			if len(out) >= maxSkillScanCount {
				return newErr(ErrInvalidManifest, "too many skills under GitHub repo; limit is %d", maxSkillScanCount)
			}
			switch entry.Type {
			case "dir":
				if skipSkillRepoDir(entry.Name) {
					continue
				}
				if err := walk(entry.Path, depth+1); err != nil {
					return err
				}
			case "file":
				cand, ok, warning := t.githubSkillCandidate(ctx, req, entry, src.Repo)
				if warning != "" {
					warnings = append(warnings, warning)
				}
				if ok {
					out = append(out, cand)
				}
			}
		}
		return nil
	}
	err := walk(src.Path, 0)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings, err
}

func (t *installSourceTool) githubSkillCandidate(ctx context.Context, req request, entry githubContentEntry, repoName string) (skillCandidate, bool, string) {
	if entry.DownloadURL == "" {
		return skillCandidate{}, false, ""
	}
	base := filepath.Base(entry.Path)
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return skillCandidate{}, false, ""
	}
	fallback := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.EqualFold(base, skill.SkillFile) {
		parent := filepath.Base(filepath.Dir(entry.Path))
		if parent == "." || parent == "" {
			parent = repoName
		}
		fallback = parent
	}
	body, err := t.fetchText(ctx, entry.DownloadURL)
	if err != nil {
		return skillCandidate{}, false, fmt.Sprintf("%s: %s", entry.DownloadURL, err.Error())
	}
	cand, err := parseSkillContent(body, fallback, entry.DownloadURL, req.strict())
	if err != nil {
		if strings.EqualFold(base, skill.SkillFile) {
			return skillCandidate{}, false, err.Error()
		}
		return skillCandidate{}, false, ""
	}
	cand.SourcePath = entry.DownloadURL
	cand.Content = body
	return cand, true, ""
}

func (t *installSourceTool) fetchGitHubContents(ctx context.Context, src githubRepoSource, branch, path string) ([]githubContentEntry, error) {
	apiURL, err := url.Parse(strings.TrimRight(githubAPIBaseURL, "/"))
	if err != nil {
		return nil, err
	}
	apiURL.Path = "/" + joinURLPath(apiURL.Path, "repos", src.Owner, src.Repo, "contents", path)
	q := apiURL.Query()
	q.Set("ref", branch)
	apiURL.RawQuery = q.Encode()
	body, err := t.fetchText(ctx, apiURL.String())
	if err != nil {
		return nil, err
	}
	var entries []githubContentEntry
	if err := json.Unmarshal([]byte(body), &entries); err == nil {
		return entries, nil
	}
	var single githubContentEntry
	if err := json.Unmarshal([]byte(body), &single); err != nil {
		return nil, newErr(ErrInvalidManifest, "%s: invalid GitHub contents response: %v", apiURL.String(), err)
	}
	return []githubContentEntry{single}, nil
}

func skipSkillRepoDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ".git", ".github", "node_modules", "references", "scripts", "assets":
		return true
	default:
		return false
	}
}

func joinURLPath(parts ...string) string {
	var cleaned []string
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func (t *installSourceTool) planLocal(req request, path string, info os.FileInfo) ([]action, []string, error) {
	var actions []action
	var warnings []string
	if req.Kind == "auto" || req.Kind == "mcp" {
		mcpPath := path
		if info.IsDir() {
			mcpPath = filepath.Join(path, ".mcp.json")
		}
		if filepath.Base(mcpPath) == ".mcp.json" {
			entries, mcpWarnings, err := readMCPJSON(mcpPath)
			if err == nil && len(entries) > 0 {
				for _, e := range entries {
					actions = append(actions, t.mcpEntryAction(req, e, mcpPath))
				}
				warnings = append(warnings, mcpWarnings...)
			} else if req.Kind == "mcp" {
				return nil, nil, err
			}
		}
		if !info.IsDir() && isExecutable(path, info) && filepath.Base(path) != ".mcp.json" {
			actions = append(actions, t.localExecutableMCPAction(req, path))
		}
	}
	if req.Kind == "auto" || req.Kind == "skill" {
		skillActions, err := t.localSkillActions(req, path, info)
		if err != nil && req.Kind == "skill" {
			return nil, nil, err
		}
		if err != nil && req.Kind == "auto" {
			warnings = append(warnings, err.Error())
		}
		actions = append(actions, skillActions...)
	}
	if len(actions) == 0 {
		return nil, warnings, newErr(ErrManifestMissing, "no installable MCP server or skill found at %s", path)
	}
	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Kind != actions[j].Kind {
			return actions[i].Kind < actions[j].Kind
		}
		return actions[i].Name < actions[j].Name
	})
	return actions, warnings, nil
}

func (t *installSourceTool) localSkillActions(req request, path string, info os.FileInfo) ([]action, error) {
	strict := req.strict()
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil, newErr(ErrUnsupportedKind, "not a markdown skill file: %s", path)
		}
		fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if strings.EqualFold(filepath.Base(path), skill.SkillFile) {
			fallback = filepath.Base(filepath.Dir(path))
		}
		cand, err := readSkillFile(path, fallback, strict)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(filepath.Base(path), skill.SkillFile) {
			cand.IsDir = true
			cand.SourcePath = filepath.Dir(path)
			cand.RootPath = filepath.Dir(filepath.Dir(path))
		} else {
			cand.RootPath = filepath.Dir(path)
		}
		if req.Name != "" {
			cand.Name = req.Name
		}
		if req.Mode == "register" {
			root := cand.RootPath
			if root == "" {
				root = filepath.Dir(path)
			}
			return []action{t.skillRootAction(req, root, []string{cand.Name})}, nil
		}
		return []action{t.skillAction(req, cand, modeForSingleSkill(req.Mode))}, nil
	}
	if st, err := os.Stat(filepath.Join(path, skill.SkillFile)); err == nil && st.Mode().IsRegular() {
		cand, err := readSkillFile(filepath.Join(path, skill.SkillFile), filepath.Base(path), strict)
		if err != nil {
			return nil, err
		}
		cand.IsDir = true
		cand.SourcePath = path
		cand.RootPath = filepath.Dir(path)
		if req.Name != "" {
			cand.Name = req.Name
		}
		if req.Mode == "register" {
			return []action{t.skillRootAction(req, filepath.Dir(path), []string{cand.Name})}, nil
		}
		return []action{t.skillAction(req, cand, modeForSingleSkill(req.Mode))}, nil
	}
	cands, err := scanSkillRoot(path, strict)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, newErr(ErrManifestMissing, "no SKILL.md or <name>.md skills found under %s", path)
	}
	mode := req.Mode
	if mode == "auto" {
		mode = "register"
	}
	if mode == "register" {
		byRoot := map[string][]string{}
		for _, cand := range cands {
			root := cand.RootPath
			if root == "" {
				root = path
			}
			byRoot[root] = append(byRoot[root], cand.Name)
		}
		roots := make([]string, 0, len(byRoot))
		for root := range byRoot {
			roots = append(roots, root)
		}
		sort.Strings(roots)
		actions := make([]action, 0, len(roots))
		for _, root := range roots {
			rootNames := byRoot[root]
			sort.Strings(rootNames)
			actions = append(actions, t.skillRootAction(req, root, rootNames))
		}
		return actions, nil
	}
	actions := make([]action, 0, len(cands))
	for _, cand := range cands {
		actions = append(actions, t.skillAction(req, cand, mode))
	}
	return actions, nil
}

// strict returns the effective strict setting, defaulting to true when the
// caller did not set the field.
func (r request) strict() bool {
	if r.Strict == nil {
		return true
	}
	return *r.Strict
}
