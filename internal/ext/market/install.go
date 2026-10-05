package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/ext/installsource"
)

var (
	// ErrUnpinned: the approved version carries no content digest, so nothing
	// could tell the reviewed material from whatever the source holds today.
	// Installing it anyway takes the person's explicit trust (Request.Trust).
	ErrUnpinned = errors.New("market: approved version is not pinned to reviewed content")
	// ErrBadSource: the approved row names something this side will not fetch —
	// a local path, a plain-http address, an unknown kind.
	ErrBadSource = errors.New("market: approved version names an uninstallable source")
	// ErrVersionChanged: the approved version is no longer the one the person
	// looked at.
	ErrVersionChanged = errors.New("market: approved version changed")
	// ErrNotTheme: a package listed as a theme would install more than themes.
	ErrNotTheme = errors.New("market: theme package carries more than themes")
)

// Request is one plan or install of a listed package. Version is the approved
// version the person was shown; empty accepts whichever is approved now, which
// only a preview does. Trust accepts an approved version no reviewer pinned;
// Digest is then the contentDigest of the preview the person confirmed.
type Request struct {
	Slug    string `json:"slug"`
	Version string `json:"version,omitempty"`
	PlanID  string `json:"planId,omitempty"`
	Replace bool   `json:"replace,omitempty"`
	Trust   bool   `json:"trust,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// Service joins the registry to install_source.
type Service struct {
	Registry Registry
	// Owner reads the signed-in account's own packages, for PlanOwn and InstallOwn.
	Owner Owner
	// Home is the Reasonix home the ledger lives under.
	Home string
	// NewInstaller returns install_source. It must require an approved plan so
	// an apply without a planId answers with the plan instead of installing.
	NewInstaller func() *installsource.Tool
	Now          func() time.Time
	// Report and InstallKey (the market's InstallID) tell the registry an
	// install landed. Either left empty sends nothing; the host leaves them
	// empty unless the person has anonymous usage statistics switched on.
	Report     InstallReporter
	InstallKey string
}

const reportTimeout = 10 * time.Second

// Outcome is install_source's own answer plus which approved version it was.
type Outcome struct {
	Fields  map[string]json.RawMessage
	Version Version
	// Unreviewed: pinned to the confirmed preview's digest, not a reviewer's.
	Unreviewed bool
}

// Plan previews the approved version of slug without writing anything.
func (s *Service) Plan(ctx context.Context, req Request) (Outcome, error) {
	return s.run(ctx, req, false)
}

// Install applies the plan req.PlanID names and records what landed.
func (s *Service) Install(ctx context.Context, req Request) (Outcome, error) {
	return s.run(ctx, req, true)
}

func (s *Service) run(ctx context.Context, req Request, apply bool) (Outcome, error) {
	detail, err := s.Registry.Detail(ctx, req.Slug)
	if err != nil {
		return Outcome{}, err
	}
	v, pinned, err := installable(detail)
	if err != nil {
		return Outcome{Version: v}, err
	}
	if req.Version != "" && req.Version != v.Version {
		return Outcome{Version: v}, fmt.Errorf("%w: shown %s, approved now %s", ErrVersionChanged, req.Version, v.Version)
	}
	if pinned {
		// Pinned since a trusted preview: what the person confirmed is not the pin.
		if digest := strings.TrimSpace(req.Digest); digest != "" && digest != v.ContentHash {
			return Outcome{Version: v}, fmt.Errorf("%w: %s was pinned since it was previewed", ErrVersionChanged, req.Slug)
		}
		return s.execute(ctx, detail.Package, v, v.ContentHash, false, false, req, apply)
	}
	if !req.Trust {
		return Outcome{Version: v}, fmt.Errorf("%w: %s@%s", ErrUnpinned, detail.Package.Slug, v.Version)
	}
	return s.previewPinned(ctx, detail.Package, v, false, req, apply)
}

// previewPinned plans or applies a version no reviewer bound a digest to. The
// preview's own digest becomes the pin, so apply lands exactly what the person
// confirmed or nothing, and a source that cannot be pinned is refused here.
func (s *Service) previewPinned(ctx context.Context, pkg Package, v Version, own bool, req Request, apply bool) (Outcome, error) {
	if apply {
		digest := strings.TrimSpace(req.Digest)
		if !installsource.IsContentDigest(digest) {
			return Outcome{Version: v}, ErrUnpreviewed
		}
		return s.execute(ctx, pkg, v, digest, true, own, req, true)
	}
	out, err := s.execute(ctx, pkg, v, "", true, own, req, false)
	if err != nil {
		return out, err
	}
	var digest string
	_ = json.Unmarshal(out.Fields["contentDigest"], &digest)
	if !installsource.IsContentDigest(digest) {
		return Outcome{Version: v}, fmt.Errorf("%w: %s@%s has no content digest to pin the install to", installsource.ErrNotPinnable, pkg.Slug, v.Version)
	}
	return out, nil
}

// execute plans or applies v of pkg through install_source, refusing material
// whose digest is not expect. Every caller has settled which version and which
// digest the person may install before reaching here; own is the publisher's.
func (s *Service) execute(ctx context.Context, pkg Package, v Version, expect string, unreviewed, own bool, req Request, apply bool) (Outcome, error) {
	body := map[string]any{
		"source":  v.Source,
		"kind":    Installer(pkg.Kind),
		"scope":   "global",
		"mode":    "copy",
		"replace": req.Replace,
		"apply":   apply,
	}
	if expect != "" {
		body["expectDigest"] = expect
	}
	if apply {
		body["planId"] = strings.TrimSpace(req.PlanID)
	}
	// A planId binds the source and actions, not the listing kind, so a theme's
	// apply re-plans and checks before anything is written.
	if pkg.Kind == "theme" {
		preview := maps.Clone(body)
		preview["apply"] = false
		delete(preview, "planId")
		if err := s.themeCheck(ctx, preview, pkg.Slug); err != nil {
			return Outcome{Version: v}, err
		}
	}
	raw, _ := json.Marshal(body)
	out, err := s.NewInstaller().Execute(ctx, raw)
	if err != nil {
		return Outcome{Version: v}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		return Outcome{Version: v}, err
	}
	if apply {
		if items := doneItems(fields["actions"]); len(items) > 0 {
			rec := Record{Slug: pkg.Slug, Kind: pkg.Kind, Version: v.Version, ContentHash: expect,
				Unreviewed: unreviewed, Items: items, At: s.now().UTC().Format(time.RFC3339)}
			if err := saveRecord(s.Home, rec); err != nil {
				fields["ledgerError"], _ = json.Marshal(err.Error())
			}
			// A publisher's own unreviewed install is not a listed package's
			// install; the registry would not count it anyway.
			if !own {
				s.report(ctx, pkg.Slug)
			}
		}
	}
	return Outcome{Fields: fields, Version: v, Unreviewed: unreviewed}, nil
}

func (s *Service) themeCheck(ctx context.Context, body map[string]any, slug string) error {
	raw, _ := json.Marshal(body)
	out, err := s.NewInstaller().Execute(ctx, raw)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		return err
	}
	if !themesOnly(fields["actions"]) {
		return fmt.Errorf("%w: %s", ErrNotTheme, slug)
	}
	return nil
}

// report is fire-and-forget: a count the registry missed is not worth holding
// the install's answer for, and outlives the request that triggered it.
func (s *Service) report(ctx context.Context, slug string) {
	if s.Report == nil || s.InstallKey == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
		defer cancel()
		_ = s.Report.ReportInstall(ctx, slug, s.InstallKey)
	}()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// installable answers the approved version, whether a reviewer pinned its
// content, and why it cannot be installed at all otherwise.
func installable(d Detail) (Version, bool, error) {
	v := d.Approved
	if v == nil {
		return Version{}, false, fmt.Errorf("%w: no row for approved version %q", ErrUnpinned, d.Package.LatestVersion)
	}
	if err := sourceInstallable(d.Package.Kind, v.Source); err != nil {
		return *v, false, err
	}
	return *v, installsource.IsContentDigest(v.ContentHash), nil
}

// sourceInstallable answers why the market would refuse to install source as
// kind, or nil. Publishing asks the same question so nothing is sent to review
// that could never be installed once approved.
func sourceInstallable(kind, source string) error {
	switch kind {
	case "plugin", "theme":
		if !commitPinned(source) {
			return fmt.Errorf("%w: %s source %q is not pinned to a commit", ErrBadSource, kind, source)
		}
	case "skill":
		if !remoteSource(source) {
			return fmt.Errorf("%w: %q", ErrBadSource, source)
		}
	case "mcp":
		if !remoteSource(source) && !npmPackage(source) {
			return fmt.Errorf("%w: %q", ErrBadSource, source)
		}
	default:
		return fmt.Errorf("%w: kind %q", ErrBadSource, kind)
	}
	return nil
}

// remoteSource admits an https URL or the git:github.com/ shorthand. A local
// path would resolve on this machine, which no reviewer has seen.
func remoteSource(s string) bool {
	if rest, ok := strings.CutPrefix(s, "git:github.com/"); ok {
		s = "https://github.com/" + rest
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// commitPinned admits a GitHub tree URL whose ref is a full commit. A branch
// ref would resolve its subpath at whatever the branch holds at apply time.
func commitPinned(s string) bool {
	if rest, ok := strings.CutPrefix(s, "git:github.com/"); ok {
		s = "https://github.com/" + rest
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) >= 4 && parts[2] == "tree" && fullCommit.MatchString(parts[3])
}

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func npmPackage(s string) bool {
	if !installsource.LooksLikePackage(s) {
		return false
	}
	name, _, _ := installsource.SplitPackageSpec(s)
	for p := range strings.SplitSeq(strings.TrimPrefix(name, "@"), "/") {
		if !slugPart(p) {
			return false
		}
	}
	return true
}

// themesOnly holds the theme category to its name: every planned action is a
// plugin package that contributes themes and nothing that runs or prompts.
func themesOnly(raw json.RawMessage) bool {
	var actions []struct {
		Kind         string          `json:"kind"`
		ThemeCount   int             `json:"themeCount"`
		SkillCount   int             `json:"skillCount"`
		AgentCount   int             `json:"agentCount"`
		CommandCount int             `json:"commandCount"`
		HookCount    int             `json:"hookCount"`
		ToolCount    int             `json:"toolCount"`
		PromptCount  int             `json:"promptCount"`
		Runtime      json.RawMessage `json:"runtime"`
	}
	if json.Unmarshal(raw, &actions) != nil || len(actions) == 0 {
		return false
	}
	for _, a := range actions {
		others := a.SkillCount + a.AgentCount + a.CommandCount + a.HookCount + a.ToolCount + a.PromptCount
		if a.Kind != "plugin" || a.ThemeCount == 0 || others != 0 || (len(a.Runtime) > 0 && string(a.Runtime) != "null") {
			return false
		}
	}
	return true
}

func doneItems(raw json.RawMessage) []Installed {
	var actions []struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Target     string `json:"target"`
		ConfigPath string `json:"configPath"`
	}
	if json.Unmarshal(raw, &actions) != nil {
		return nil
	}
	var out []Installed
	for _, a := range actions {
		if a.Status != "done" {
			continue
		}
		target := a.Target
		if a.Kind == "mcp" {
			target = a.ConfigPath
		}
		out = append(out, Installed{Kind: a.Kind, Name: a.Name, Target: target})
	}
	return out
}

// NewInstallSource is the installer a host gives Service: install_source with
// the approved-plan ticket required.
func NewInstallSource(projectRoot string, hc *http.Client, onDisconnect installsource.OnDisconnectFunc) func() *installsource.Tool {
	return func() *installsource.Tool {
		return installsource.NewTool(installsource.Options{
			ProjectRoot:         projectRoot,
			HTTPClient:          hc,
			OnDisconnect:        onDisconnect,
			RequireApprovedPlan: true,
		})
	}
}
