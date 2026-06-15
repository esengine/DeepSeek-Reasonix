package installsource

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/frontmatter"
	"reasonix/internal/skill"
)

const (
	maxSkillScanDepth = 3
	maxSkillScanCount = 200
	maxSkillCopyBytes = 20 << 20
)

// skillAction builds the DTO for a single-skill install (copy or link).
func (t *installSourceTool) skillAction(req request, cand skillCandidate, mode string) action {
	actionName := "copy_skill"
	if mode == "link" {
		actionName = "link_skill"
	}
	canonical, _ := t.skillCanonicalPath(cand.Name, req.Scope)
	root, _ := t.skillInstallRoot(req.Scope)
	a := action{
		Kind:          "skill",
		Action:        actionName,
		Name:          cand.Name,
		Source:        cand.SourcePath,
		Target:        canonical,
		Scope:         req.Scope,
		Mode:          mode,
		ConfigPath:    t.configPath(req.Scope),
		Skills:        []string{cand.Name},
		SkillCount:    1,
		Layout:        "canonical_dir",
		InstallRoot:   root,
		CanonicalPath: canonical,
		skill:         cand,
	}
	a.RiskLevel, a.RiskReasons = skillActionRisk(mode, cand)
	if mode == "link" && !isLinkTargetSafe(cand.SourcePath, t.home, t.root) {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "link target is an absolute path outside the project or home root")
	}
	return a
}

// skillActionRisk explains the risk budget for a skill install. The model
// uses this to decide whether to call apply=true directly or to ask first.
func skillActionRisk(mode string, cand skillCandidate) (RiskLevel, []string) {
	reasons := []string{}
	level := RiskLow
	if mode == "link" {
		// Link installs a pointer into a foreign tree; an untrusted source
		// could expose anything at runtime, so we always classify as medium
		// at minimum.
		level = RiskMedium
		reasons = append(reasons, "symlink to a foreign path")
	}
	if cand.IsDir && mode == "copy" {
		if level == RiskLow {
			level = RiskMedium
		}
		reasons = append(reasons, "copy of a directory")
	}
	return level, reasons
}

// skillRootAction builds the DTO for registering a whole skill directory.
func (t *installSourceTool) skillRootAction(req request, path string, names []string) action {
	return action{
		Kind:        "skill",
		Action:      "register_skill_root",
		Name:        "",
		Source:      path,
		Target:      path,
		ConfigPath:  t.configPath(req.Scope),
		Scope:       req.Scope,
		Mode:        "register",
		Skills:      names,
		SkillCount:  len(names),
		Layout:      "registered_root",
		InstallRoot: path,
		RiskLevel:   RiskMedium,
		RiskReasons: []string{"adds a new skill root to the active config"},
	}
}

func (t *installSourceTool) skillInstallRoot(scope string) (string, error) {
	if scope == "global" {
		if t.home == "" {
			return "", newErr(ErrSourceUnreadable, "global skill install requires a home directory")
		}
		return filepath.Join(t.home, ".reasonix", skill.SkillsDirname), nil
	}
	return filepath.Join(t.root, ".reasonix", skill.SkillsDirname), nil
}

// skillCanonicalPath computes the canonical install destination:
// <scope>/skills/<skill-name>/SKILL.md. Flat <name>.md remains readable for
// backward compatibility, but the installer no longer writes it by default.
func (t *installSourceTool) skillCanonicalPath(name, scope string) (string, error) {
	if !config.IsValidSkillName(name) {
		return "", newErr(ErrInvalidManifest, "invalid skill name %q", name)
	}
	root, err := t.skillInstallRoot(scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name, skill.SkillFile), nil
}

// verifySkill confirms the installed skill is reachable through a freshly
// built Store. It is the post-install guard against partial failures.
func (t *installSourceTool) verifySkill(scope, name string, act *action) error {
	custom := []string(nil)
	if scope == "project" {
		cfg := config.LoadForEdit(filepath.Join(t.root, "reasonix.toml"))
		custom = cfg.SkillCustomPaths()
	} else {
		cfg := config.LoadForEdit(t.configPath(scope))
		custom = cfg.SkillCustomPaths()
	}
	var stderr bytes.Buffer
	store := skill.New(skill.Options{HomeDir: t.home, ProjectRoot: t.root, CustomPaths: custom, DisableBuiltins: true, Stderr: &stderr})
	sk, ok := store.Read(name)
	if !ok {
		return newErr(ErrSourceUnreadable, "skill %q is installed but not discoverable", name)
	}
	act.Discoverable = true
	act.CanonicalPath = sk.Path
	for _, listed := range store.List() {
		if listed.Name == name {
			act.Indexed = true
			break
		}
	}
	if strings.TrimSpace(sk.Description) == "" {
		act.Warnings = append(act.Warnings, fmt.Sprintf("skill %q has no description frontmatter; it is installed but the skills index will use a placeholder", name))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		act.Warnings = append(act.Warnings, msg)
	}
	return nil
}

// skillConflictTargets returns every existing layout that would collide with
// installing name: the canonical directory, its SKILL.md, and the legacy flat
// file. The apply step checks all of them so new canonical installs don't
// silently shadow older <name>.md installs.
func (t *installSourceTool) skillConflictTargets(name, scope string) ([]string, error) {
	canonical, err := t.skillCanonicalPath(name, scope)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(canonical)
	return []string{dir, canonical, filepath.Join(filepath.Dir(dir), name+".md")}, nil
}

// readSkillFile reads and validates a single skill file. The fallback name
// is used when the frontmatter does not declare one.
func readSkillFile(path, fallbackName string, strict bool) (skillCandidate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return skillCandidate{}, err
	}
	cand, err := parseSkillContent(string(b), fallbackName, path, strict)
	if err != nil {
		return skillCandidate{}, err
	}
	cand.SourcePath = path
	return cand, nil
}

// parseSkillContent validates the YAML frontmatter of a skill file. With
// strict=true (the default) we require a `name` and a `description`; with
// strict=false a missing description is allowed and the body may be empty —
// useful for installing raw files the user already trusts.
func parseSkillContent(content, fallbackName, source string, strict bool) (skillCandidate, error) {
	bom := "\uFEFF"
	content = strings.TrimPrefix(strings.ReplaceAll(content, "\r\n", "\n"), bom)
	fm, body := frontmatter.Split(content)
	name := strings.TrimSpace(fallbackName)
	if v := strings.TrimSpace(fm["name"]); v != "" {
		name = v
	}
	if !config.IsValidSkillName(name) {
		return skillCandidate{}, newErr(ErrInvalidManifest, "skill %q at %s has an invalid name", name, source)
	}
	desc := collapseSpaces(fm["description"])
	if strict {
		if desc == "" {
			return skillCandidate{}, newErr(ErrInvalidManifest, "skill %q at %s is missing description frontmatter", name, source)
		}
		if strings.TrimSpace(body) == "" {
			return skillCandidate{}, newErr(ErrInvalidManifest, "skill %q at %s has an empty body", name, source)
		}
	}
	return skillCandidate{Name: name, Description: desc, SourcePath: source, Content: content}, nil
}

// scanSkillRoot enumerates skills under a directory with bounded recursion:
// any <name>/SKILL.md is a directory-layout skill, and any <name>.md is a
// flat compatibility skill. RootPath records the containing directory that must
// be registered for the runtime Store to discover that candidate.
func scanSkillRoot(root string, strict bool) ([]skillCandidate, error) {
	var out []skillCandidate
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := pathDepth(rel)
		if d.IsDir() {
			if depth > maxSkillScanDepth {
				return filepath.SkipDir
			}
			if strings.EqualFold(d.Name(), ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= maxSkillScanCount {
			return newErr(ErrInvalidManifest, "too many skills under %s; limit is %d", root, maxSkillScanCount)
		}
		if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		parent := filepath.Dir(path)
		if strings.EqualFold(d.Name(), skill.SkillFile) {
			containerDepth := pathDepth(mustRel(root, parent))
			if containerDepth > maxSkillScanDepth {
				return nil
			}
			if parent == root {
				return nil
			}
			cand, err := readSkillFile(path, filepath.Base(parent), strict)
			if err == nil {
				cand.IsDir = true
				cand.SourcePath = parent
				cand.RootPath = filepath.Dir(parent)
				out = append(out, cand)
			}
			return nil
		}
		containerDepth := pathDepth(mustRel(root, parent))
		if containerDepth > maxSkillScanDepth {
			return nil
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		cand, err := readSkillFile(path, stem, strict)
		if err == nil {
			cand.RootPath = parent
			out = append(out, cand)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func pathDepth(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return len(strings.Split(filepath.Clean(rel), string(filepath.Separator)))
}

func mustRel(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "."
	}
	return rel
}

// copyDir walks src and writes a parallel tree under dst. O_EXCL refuses to
// overwrite a leaf; a leftover partial tree is left on disk for the user to
// inspect (we never rm -rf). Symlinks inside src are followed once and
// copied as the resolved file, which is what skill directories expect.
func copyDir(src, dst string) error {
	var copied int64
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if strings.EqualFold(d.Name(), ".git") {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		copied += info.Size()
		if copied > maxSkillCopyBytes {
			return newErr(ErrInvalidManifest, "skill directory exceeds %d bytes", maxSkillCopyBytes)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func writeNewFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return err
	}
	return nil
}
