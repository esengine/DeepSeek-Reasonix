package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/workspaceid"
)

// A project file can name programs the host runs: hooks, language servers,
// ripgrep, the shell. Each runs only once the user approved that exact
// declaration; approvals live under the user's home, never in the checkout,
// keyed by workspace folder and content digest.
const (
	projectProgramsFilename = "project-programs.json"
	projectProgramsLockFile = ".project-programs.lock"
	projectProgramsVersion  = 1
)

// ProjectProgramKind names which kind of host-run program a project declares.
type ProjectProgramKind string

const (
	ProjectProgramHooks   ProjectProgramKind = "hooks"
	ProjectProgramLSP     ProjectProgramKind = "lsp"
	ProjectProgramRipgrep ProjectProgramKind = "rg_path"
	ProjectProgramShell   ProjectProgramKind = "shell_path"
	ProjectProgramBrowser ProjectProgramKind = "browser"
	ProjectProgramLaya    ProjectProgramKind = "laya_python"
)

// ErrProjectProgramsUnavailable is a record that could not be read or written.
var ErrProjectProgramsUnavailable = errors.New("project program approvals unavailable")

// ProjectProgram is one project-declared program as the user would approve it.
// Detail is a one-line summary; Declaration and Files are everything Digest
// covers, so what a person is shown to approve is exactly what is checked.
type ProjectProgram struct {
	Kind        ProjectProgramKind `json:"kind"`
	Name        string             `json:"name"`
	Detail      string             `json:"detail"`
	Declaration string             `json:"declaration"`
	Files       []string           `json:"files,omitempty"`
	Digest      string             `json:"digest"`
}

// NewProjectProgram digests decl together with every word in words that names
// a regular file inside ws (relative words resolve against base).
func NewProjectProgram(kind ProjectProgramKind, name, detail, ws, base string, decl any, words []string) ProjectProgram {
	body, _ := json.Marshal(struct {
		Kind ProjectProgramKind
		Name string
		Decl any
	}{kind, name, decl})
	if strings.TrimSpace(detail) == "" {
		detail = name
	}
	p := ProjectProgram{Kind: kind, Name: name, Detail: detail, Declaration: string(body), Files: workspaceFilesNamed(ws, base, words)}
	p.Digest = p.currentDigest()
	return p
}

// currentDigest hashes the declaration and the files' content as they are now;
// a file that can no longer be read hashes differently from its approved self.
func (p ProjectProgram) currentDigest() string {
	h := sha256.New()
	h.Write([]byte(p.Declaration))
	for _, file := range p.Files {
		fmt.Fprintf(h, "\x00%s\x00", file)
		if f, err := os.Open(file); err == nil {
			_, _ = io.Copy(h, f)
			_ = f.Close()
		} else {
			h.Write([]byte("\x00unreadable"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// workspaceFilesNamed lists the files words name that what runs in the
// workspace could rewrite: any relative path, taken as the OS resolves it
// (a link, then ".."), and any absolute one in the workspace or a directory
// the bash jail leaves writable. A path missing now is still listed, so
// creating it later changes the digest.
func workspaceFilesNamed(ws, base string, words []string) []string {
	if ws == "" {
		return nil
	}
	if base == "" {
		base = ws
	}
	var out []string
	for _, word := range namedWords(words) {
		path := word
		if !filepath.IsAbs(path) {
			path = base + string(filepath.Separator) + word
		} else if !strings.Contains(word, "..") && !inRoots(path, append([]string{ws, os.TempDir()}, hostWritableDirs()...)) {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.Mode().IsRegular() || err != nil && !strings.ContainsAny(word, `/\.`) {
			continue
		}
		if !slices.Contains(out, path) {
			out = append(out, path)
		}
	}
	return out
}

// namedWords yields each word and, for a flag like --require=./x, its value.
func namedWords(words []string) []string {
	var out []string
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		out = append(out, word)
		if _, value, ok := strings.Cut(word, "="); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

type programApproval struct {
	Workspace  string             `json:"workspace"`
	Kind       ProjectProgramKind `json:"kind"`
	Name       string             `json:"name"`
	Digest     string             `json:"digest"`
	ApprovedAt time.Time          `json:"approved_at"`
}

type programApprovalFile struct {
	Version   int               `json:"version"`
	Approvals []programApproval `json:"approvals"`
}

// ProjectProgramStore persists approvals in <Reasonix home>/project-programs.json.
type ProjectProgramStore struct {
	path string
	mu   sync.Mutex
}

// NewProjectProgramStore opens the approval record under home.
func NewProjectProgramStore(home string) *ProjectProgramStore {
	if strings.TrimSpace(home) == "" {
		return &ProjectProgramStore{}
	}
	return &ProjectProgramStore{path: filepath.Join(home, projectProgramsFilename)}
}

// Approved reports whether p, exactly as digested, was approved for root.
func (s *ProjectProgramStore) Approved(root string, p ProjectProgram) (bool, error) {
	file, err := s.load()
	if err != nil {
		return false, err
	}
	ws := workspaceid.PathFingerprint(root)
	return slices.ContainsFunc(file.Approvals, func(a programApproval) bool {
		return ws != "" && a.Workspace == ws && a.Kind == p.Kind && a.Name == p.Name && a.Digest == p.Digest
	}), nil
}

// Approve records programs for root, replacing an earlier digest of the same one.
func (s *ProjectProgramStore) Approve(root string, programs ...ProjectProgram) error {
	ws := workspaceid.PathFingerprint(root)
	if ws == "" {
		return fmt.Errorf("%w: no workspace to approve for", ErrProjectProgramsUnavailable)
	}
	return s.update(func(file *programApprovalFile) {
		for _, p := range programs {
			file.Approvals = slices.DeleteFunc(file.Approvals, func(a programApproval) bool {
				return a.Workspace == ws && a.Kind == p.Kind && a.Name == p.Name
			})
			file.Approvals = append(file.Approvals, programApproval{Workspace: ws, Kind: p.Kind, Name: p.Name, Digest: p.Digest, ApprovedAt: time.Now().UTC()})
		}
	})
}

// Revoke drops every approval recorded for root.
func (s *ProjectProgramStore) Revoke(root string) error {
	ws := workspaceid.PathFingerprint(root)
	return s.update(func(file *programApprovalFile) {
		file.Approvals = slices.DeleteFunc(file.Approvals, func(a programApproval) bool { return a.Workspace == ws })
	})
}

func (s *ProjectProgramStore) load() (programApprovalFile, error) {
	if s == nil || s.path == "" {
		return programApprovalFile{}, fmt.Errorf("%w: no Reasonix home", ErrProjectProgramsUnavailable)
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return programApprovalFile{Version: projectProgramsVersion}, nil
	}
	if err != nil {
		return programApprovalFile{}, fmt.Errorf("%w: %w", ErrProjectProgramsUnavailable, err)
	}
	var file programApprovalFile
	if err := json.Unmarshal(data, &file); err != nil {
		return programApprovalFile{}, fmt.Errorf("%w: %s: %w", ErrProjectProgramsUnavailable, s.path, err)
	}
	return file, nil
}

func (s *ProjectProgramStore) update(mutate func(*programApprovalFile)) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("%w: no Reasonix home", ErrProjectProgramsUnavailable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("%w: %w", ErrProjectProgramsUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, filepath.Join(filepath.Dir(s.path), projectProgramsLockFile))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProjectProgramsUnavailable, err)
	}
	defer unlock()
	file, err := s.load()
	if err != nil {
		return err
	}
	mutate(&file)
	file.Version = projectProgramsVersion
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProjectProgramsUnavailable, err)
	}
	if err := fileutil.AtomicWriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("%w: %w", ErrProjectProgramsUnavailable, err)
	}
	return nil
}

// PendingProjectPrograms lists the project-declared programs this load held
// back because nobody approved them.
func (c *Config) PendingProjectPrograms() []ProjectProgram {
	if c == nil {
		return nil
	}
	return slices.Clone(c.projectScope.pending)
}

// admit reports whether a project program may run, recording why not.
func (c *Config) admit(store *ProjectProgramStore, ws, key string, p ProjectProgram) bool {
	ok, err := store.Approved(ws, p)
	switch {
	case err != nil:
		c.ignoreProject(key, p.Detail, ProjectApprovalUnavailable)
	case !ok:
		c.ignoreProject(key, p.Detail, ProjectAwaitingApproval)
	}
	if ok {
		c.projectScope.admitted = append(c.projectScope.admitted, p)
	} else {
		c.projectScope.pending = append(c.projectScope.pending, p)
	}
	return ok
}

// gatePrograms keeps a project-chosen shell, ripgrep and language server only
// once approved; otherwise the user's own value stays in force.
func (h heldScope) gatePrograms(c *Config, store *ProjectProgramStore, ws string) {
	singles := []struct {
		kind ProjectProgramKind
		key  string
		v    *string
		held string
	}{
		{ProjectProgramShell, "tools.shell.path", &c.Tools.Shell.Path, h.shell.Path},
		{ProjectProgramRipgrep, "tools.search.rg_path", &c.Tools.Search.RgPath, h.rgPath},
		{ProjectProgramBrowser, "browser.executable", &c.Browser.Executable, h.browser.Executable},
		{ProjectProgramLaya, "tools.system_one.laya.python", &c.Tools.SystemOne.Laya.Python, h.layaPython},
	}
	for _, one := range singles {
		if *one.v == one.held {
			continue
		}
		// A single binary where sandboxed commands can write could be replaced
		// after it was approved and before the host starts it again.
		if c.namesWritablePath(ws, *one.v) {
			c.ignoreProject(one.key, *one.v, ProjectProgramWritable)
			*one.v = one.held
			continue
		}
		p := newSingleProgram(one.kind, one.key, *one.v, ws)
		if !c.admit(store, ws, one.key, p) {
			*one.v = one.held
		}
	}
	servers := maps.Clone(h.lsp)
	for _, lang := range slices.Sorted(maps.Keys(c.LSP.Servers)) {
		srv := c.LSP.Servers[lang]
		if user, ok := h.lsp[lang]; ok && reflect.DeepEqual(user, srv) {
			continue
		}
		detail := strings.TrimSpace(strings.Join(append([]string{srv.Command}, srv.Args...), " ") + EnvSummary(srv.Env))
		p := NewProjectProgram(ProjectProgramLSP, lang, detail, ws, ws, srv, append([]string{srv.Command}, srv.Args...))
		if c.admit(store, ws, "lsp.servers."+lang, p) {
			if servers == nil {
				servers = map[string]LSPServer{}
			}
			servers[lang] = srv
		}
	}
	c.LSP.Servers = servers
}

// ApproveWorkspacePrograms approves every program root's configuration names,
// as it stands now, and returns them. It is what a person confirming the whole
// list does; project hooks are the hook package's to approve.
func (r Roots) ApproveWorkspacePrograms(root string) ([]ProjectProgram, error) {
	cfg, err := r.LoadForRootReadOnly(root)
	if err != nil {
		return nil, err
	}
	pending := cfg.PendingProjectPrograms()
	if len(pending) == 0 {
		return nil, nil
	}
	return pending, NewProjectProgramStore(r.Home()).Approve(root, pending...)
}

// EnvSummary renders env for a person approving it, in a stable order.
func EnvSummary(env map[string]string) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(env)) {
		fmt.Fprintf(&b, " %s=%s", k, env[k])
	}
	return b.String()
}
