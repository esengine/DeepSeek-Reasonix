package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	"reasonix/internal/skill/builtincontent"
)

const maxBuiltinSkillUpdateBytes = 256 << 10
const builtinSkillCheckFreshness = 5 * time.Minute

type builtinSkillSource struct {
	id, url, upstreamName string
}

var builtinSkillSources = []builtinSkillSource{
	{"vision-liao-memory", "https://raw.githubusercontent.com/topoteretes/cognee/main/cognee/skill.md", "cognee"},
	{"ponytail", "https://raw.githubusercontent.com/DietrichGebert/ponytail/main/skills/ponytail/SKILL.md", "ponytail"},
	{"taste-skill", "https://raw.githubusercontent.com/Leonxlnx/taste-skill/main/skills/taste-skill/SKILL.md", "design-taste-frontend"},
	{"zerotoken-skill", "https://raw.githubusercontent.com/phoenixlucky/zerotoken-skill/main/SKILL.md", "zerotoken-skill"},
	{"weiliaozi-skill", "https://raw.githubusercontent.com/phoenixlucky/weiliaozi-skill/main/SKILL.md", "weiliaozi-skill"},
	{"moon-lovers", "https://raw.githubusercontent.com/phoenixlucky/moon-lovers-skill/main/SKILL.md", "moon-lovers-skill"},
	{"officecli", "https://raw.githubusercontent.com/iOfficeAI/OfficeCLI/main/SKILL.md", "officecli"},
}

var builtinSkillUpdateMu sync.Mutex

// BuiltinSkillUpdateInfo contains safe update status for a shipped Skill.
type BuiltinSkillUpdateInfo struct {
	ID               string `json:"id"`
	Supported        bool   `json:"supported"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	LatestVersion    string `json:"latestVersion,omitempty"`
	UpdateAvailable  bool   `json:"updateAvailable"`
	LastCheckedAt    string `json:"lastCheckedAt,omitempty"`
	Error            string `json:"error,omitempty"`
}

type savedBuiltinSkill struct {
	ID               string `json:"id"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	LatestVersion    string `json:"latestVersion,omitempty"`
	LastCheckedAt    string `json:"lastCheckedAt,omitempty"`
	Body             string `json:"body,omitempty"`
}

// BuiltinSkillUpdates returns update status for every embedded WJ Forge Skill.
func BuiltinSkillUpdates() []BuiltinSkillUpdateInfo {
	items, _ := builtincontent.All()
	out := make([]BuiltinSkillUpdateInfo, 0, len(items))
	for _, item := range items {
		out = append(out, builtinSkillUpdateInfo(item.Name, ""))
	}
	return out
}

// CheckBuiltinSkillUpdates checks only the fixed HTTPS sources compiled above.
func CheckBuiltinSkillUpdates(ctx context.Context) []BuiltinSkillUpdateInfo {
	builtinSkillUpdateMu.Lock()
	defer builtinSkillUpdateMu.Unlock()
	out := make([]BuiltinSkillUpdateInfo, 0, len(builtinSkillSources))
	results := make([]BuiltinSkillUpdateInfo, len(builtinSkillSources))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				source := builtinSkillSources[i]
				version, _, err := fetchBuiltinSkill(ctx, source)
				if err == nil {
					var saved savedBuiltinSkill
					saved, err = readBuiltinSkill(source.id)
					if err == nil {
						saved.ID, saved.LatestVersion = source.id, version
						saved.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
						err = writeBuiltinSkill(source.id, saved)
					}
				}
				results[i] = builtinSkillUpdateInfo(source.id, errorString(err))
			}
		}()
	}
	for i := range builtinSkillSources {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	out = append(out, results...)
	return out
}

// UpdateBuiltinSkill fetches, validates, and atomically installs one known source.
func UpdateBuiltinSkill(ctx context.Context, id string) (BuiltinSkillUpdateInfo, error) {
	builtinSkillUpdateMu.Lock()
	defer builtinSkillUpdateMu.Unlock()
	var source *builtinSkillSource
	for i := range builtinSkillSources {
		if builtinSkillSources[i].id == id {
			source = &builtinSkillSources[i]
			break
		}
	}
	if source == nil {
		return BuiltinSkillUpdateInfo{}, fmt.Errorf("skill %q has no validated update source", id)
	}
	saved, err := readBuiltinSkill(id)
	if err != nil {
		return builtinSkillUpdateInfo(id, err.Error()), err
	}
	if !hasFreshBuiltinSkillCheck(saved) {
		return builtinSkillUpdateInfo(id, ""), fmt.Errorf("check for Skill updates again before installing")
	}
	if saved.LatestVersion == saved.InstalledVersion {
		return builtinSkillUpdateInfo(id, ""), fmt.Errorf("no checked Skill update is available")
	}
	version, content, err := fetchBuiltinSkill(ctx, *source)
	if err != nil {
		return builtinSkillUpdateInfo(id, err.Error()), err
	}
	if version != saved.LatestVersion {
		err := fmt.Errorf("Skill source changed since the last check; check again before installing")
		return builtinSkillUpdateInfo(id, err.Error()), err
	}
	saved.ID, saved.InstalledVersion, saved.LatestVersion = id, version, version
	saved.LastCheckedAt, saved.Body = time.Now().UTC().Format(time.RFC3339), content
	if err := writeBuiltinSkill(id, saved); err != nil {
		return builtinSkillUpdateInfo(id, err.Error()), err
	}
	return builtinSkillUpdateInfo(id, ""), nil
}

func fetchBuiltinSkill(parent context.Context, source builtinSkillSource) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 5 {
			return fmt.Errorf("skill update redirect rejected")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("skill update request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("skill update returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBuiltinSkillUpdateBytes {
		return "", "", fmt.Errorf("skill update exceeds 256 KiB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBuiltinSkillUpdateBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read skill update: %w", err)
	}
	if len(data) > maxBuiltinSkillUpdateBytes {
		return "", "", fmt.Errorf("skill update exceeds 256 KiB")
	}
	parsed, err := builtincontent.ParseSkillMarkdown(source.id+"/SKILL.md", string(data))
	if err != nil {
		return "", "", err
	}
	if parsed.Name != source.upstreamName || strings.TrimSpace(parsed.Description) == "" || parsed.Body == "" {
		return "", "", fmt.Errorf("skill update identity or required content did not match")
	}
	for key := range parsed.Frontmatter {
		switch strings.ToLower(key) {
		case "name", "description", "category", "source-url":
		default:
			return "", "", fmt.Errorf("skill update contains unsupported frontmatter")
		}
	}
	version := strings.TrimSpace(resp.Header.Get("ETag"))
	if version == "" {
		sum := sha256.Sum256(data)
		version = hex.EncodeToString(sum[:])
	}
	return version, parsed.Body, nil
}

func builtinSkillUpdateInfo(id, updateErr string) BuiltinSkillUpdateInfo {
	saved, err := readBuiltinSkill(id)
	if err != nil && updateErr == "" {
		updateErr = err.Error()
	}
	supported := false
	for _, source := range builtinSkillSources {
		supported = supported || source.id == id
	}
	info := BuiltinSkillUpdateInfo{ID: id, Supported: supported, InstalledVersion: saved.InstalledVersion, LatestVersion: saved.LatestVersion, LastCheckedAt: saved.LastCheckedAt, Error: updateErr}
	info.UpdateAvailable = supported && hasFreshBuiltinSkillCheck(saved) && info.LatestVersion != info.InstalledVersion && info.Error == ""
	return info
}

func hasFreshBuiltinSkillCheck(saved savedBuiltinSkill) bool {
	checkedAt, err := time.Parse(time.RFC3339, saved.LastCheckedAt)
	return err == nil && saved.LatestVersion != "" && time.Since(checkedAt) <= builtinSkillCheckFreshness && !checkedAt.After(time.Now().Add(time.Minute))
}

func readBuiltinSkill(id string) (savedBuiltinSkill, error) {
	var saved savedBuiltinSkill
	data, err := os.ReadFile(builtinSkillUpdatePath(id))
	if os.IsNotExist(err) {
		return saved, nil
	}
	if err != nil {
		return saved, fmt.Errorf("read saved Skill update: %w", err)
	}
	if len(data) > maxBuiltinSkillUpdateBytes+8192 {
		return saved, fmt.Errorf("saved Skill update is too large")
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.ID != id {
		return savedBuiltinSkill{}, fmt.Errorf("saved Skill update is invalid")
	}
	return saved, nil
}

func writeBuiltinSkill(id string, saved savedBuiltinSkill) error {
	path := builtinSkillUpdatePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create Skill update directory: %w", err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("install Skill update: %w", err)
	}
	return nil
}

func builtinSkillUpdatePath(id string) string {
	if !IsValidName(id) {
		return filepath.Join(config.ReasonixHomeDir(), "skill-updates", "invalid")
	}
	return filepath.Join(config.ReasonixHomeDir(), "skill-updates", id+".json")
}

func applyBuiltinSkillUpdates(skills []Skill, home string) []Skill {
	for i := range skills {
		if skills[i].Scope != ScopeBuiltin {
			continue
		}
		path := builtinSkillUpdatePathAt(home, skills[i].Name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxBuiltinSkillUpdateBytes+8192 {
			continue
		}
		var saved savedBuiltinSkill
		if json.Unmarshal(data, &saved) == nil && saved.ID == skills[i].Name && saved.Body != "" {
			skills[i].Body = saved.Body
		}
	}
	return skills
}

func builtinSkillUpdatePathAt(home, id string) string {
	if !IsValidName(id) {
		return ""
	}
	if home == "" {
		home = config.ReasonixHomeDir()
	}
	return filepath.Join(home, "skill-updates", id+".json")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
