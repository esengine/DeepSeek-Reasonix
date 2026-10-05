package configbackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
)

func TestMain(m *testing.M) { testenv.RunWithIsolatedUserState(m) }

const passphrase = "correct horse battery"

const userConfig = `
default_model = "work/model-a"

[statusline]
command = "/usr/local/bin/status --fast"

[[providers]]
name = "work"
kind = "openai"
base_url = "https://api.example.com/v1"
api_key_env = "WORK_API_KEY"
models = ["model-a"]
headers = { X-Org-Token = "literal-header-secret" }

[[plugins]]
name = "files"
command = "npx"
args = ["-y", "files-server"]
env = { FILES_TOKEN = "literal-env-secret" }
`

// machine points every user root at a fresh directory and writes a config.
func machine(t *testing.T, cfg string) string {
	t.Helper()
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(dir, "home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("WORK_API_KEY", "")
	os.Unsetenv("WORK_API_KEY")
	must(t, os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755))
	must(t, os.WriteFile(config.UserConfigPath(), []byte(cfg), 0o600))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func collectAll(t *testing.T, cats ...Category) *Snapshot {
	t.Helper()
	s, err := Collect(CollectOptions{Categories: cats, AppVersion: "test"})
	must(t, err)
	return s
}

func TestSealOpenRoundTripAndRejections(t *testing.T) {
	machine(t, userConfig)
	s := collectAll(t, CategorySettings)
	sealed, err := Seal(s, passphrase)
	must(t, err)
	if bytes.Contains(sealed, []byte("api.example.com")) {
		t.Fatal("sealed backup carries plaintext")
	}
	back, err := Open(sealed, passphrase)
	must(t, err)
	if len(back.Items) != len(s.Items) || back.Format != FormatVersion {
		t.Fatalf("round trip lost items: %d vs %d", len(back.Items), len(s.Items))
	}
	if _, err := Open(sealed, "wrong passphrase!"); !errors.Is(err, ErrCannotDecrypt) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	tampered := slices.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	if _, err := Open(tampered, passphrase); !errors.Is(err, ErrCannotDecrypt) {
		t.Fatalf("tampered ciphertext: %v", err)
	}
	// The header is associated data: lowering the KDF cost must not decrypt.
	h, n, err := parseHeader(sealed)
	must(t, err)
	h.Time = 1
	prefix, err := envelopePrefix(h)
	must(t, err)
	if _, err := Open(append(prefix, sealed[n:]...), passphrase); !errors.Is(err, ErrCannotDecrypt) {
		t.Fatalf("rewritten header: %v", err)
	}
	h.Memory = 1 << 30
	prefix, _ = envelopePrefix(h)
	if _, err := Open(append(prefix, sealed[n:]...), passphrase); !errors.Is(err, ErrMalformed) {
		t.Fatalf("hostile KDF memory: %v", err)
	}
	if _, err := Seal(s, "short"); !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("short passphrase: %v", err)
	}
}

func TestCollectWithoutSecretsStripsLiteralCredentials(t *testing.T) {
	machine(t, userConfig)
	_, err := config.SetCredential("WORK_API_KEY", "sk-stored-key")
	must(t, err)
	_, err = config.SetCredential(accountTokenKey, "session-token")
	must(t, err)
	s := collectAll(t, CategorySettings, CategoryExtensions, CategoryAutomation)
	raw, _ := json.Marshal(s)
	for _, secret := range []string{"literal-header-secret", "literal-env-secret", "sk-stored-key", "session-token"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("snapshot without secrets carries %q", secret)
		}
	}
	withSecrets := collectAll(t, CategorySettings, CategorySecrets)
	raw, _ = json.Marshal(withSecrets)
	if !bytes.Contains(raw, []byte("sk-stored-key")) || !bytes.Contains(raw, []byte("literal-header-secret")) {
		t.Fatal("secrets category left the stored key out")
	}
	if bytes.Contains(raw, []byte("session-token")) {
		t.Fatal("the account session token must never travel")
	}
}

func TestRestoreRefusesExecutingItemsWithoutConsent(t *testing.T) {
	machine(t, userConfig)
	must(t, hook.Save(hook.ScopeGlobal, "", hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		"PreToolUse": {{Match: "bash", Command: "echo guarded"}},
	}}))
	s := collectAll(t, CategoryAutomation, CategoryExtensions)

	machine(t, "")
	p := NewPlanner()
	plan, err := p.Preview(s)
	must(t, err)
	var hookID string
	for _, it := range plan.Items {
		if it.Kind == KindHook {
			hookID = it.ID
			if it.Consent != ConsentExecutes || it.Recommended {
				t.Fatalf("hook row: %+v", it)
			}
		}
	}
	_, err = p.Apply(ApplyRequest{PlanID: plan.ID, Items: []string{hookID, "statusline:statusline", "mcp:files"}})
	var ce *ConsentError
	if !errors.As(err, &ce) || !errors.Is(err, ErrConsentRequired) || len(ce.IDs) != 3 {
		t.Fatalf("apply without consent: %v", err)
	}
	if got, _ := globalHooks(); len(got.Hooks) != 0 {
		t.Fatal("a refused apply wrote hooks")
	}
	res, err := p.Apply(ApplyRequest{PlanID: plan.ID, Items: []string{hookID}, Consented: []string{hookID}})
	must(t, err)
	if len(res.Applied) != 1 || len(res.Failed) != 0 {
		t.Fatalf("consented apply: %+v", res)
	}
	got, _ := globalHooks()
	if len(got.Hooks["PreToolUse"]) != 1 || got.Hooks["PreToolUse"][0].Command != "echo guarded" {
		t.Fatalf("hook not restored: %+v", got)
	}
	if _, err := p.Apply(ApplyRequest{PlanID: plan.ID, Items: []string{hookID}, Consented: []string{hookID}}); !errors.Is(err, ErrPlanExpired) {
		t.Fatalf("a preview backs one apply: %v", err)
	}
}

func TestProviderRedirectNeedsConsentAndLeavesLocalHeadersBehind(t *testing.T) {
	machine(t, userConfig)
	s := collectAll(t, CategorySettings)
	machine(t, strings.Replace(userConfig, "api.example.com", "elsewhere.example.net", 1))
	p := NewPlanner()
	plan, err := p.Preview(s)
	must(t, err)
	row := planRow(t, plan, "provider:work")
	if row.Status != StatusChanged || row.Consent != ConsentEndpoint {
		t.Fatalf("provider row: %+v", row)
	}
	if _, err := p.Apply(ApplyRequest{PlanID: plan.ID, Items: []string{row.ID}}); !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("redirect without consent: %v", err)
	}
	_, err = p.Apply(ApplyRequest{PlanID: plan.ID, Items: []string{row.ID}, Consented: []string{row.ID}})
	must(t, err)
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	must(t, err)
	// The local literal belonged to the old endpoint; carrying it to the new one
	// would be the redirect the consent was about, so the placeholder stays.
	if cfg.Providers[0].BaseURL != "https://api.example.com/v1" || cfg.Providers[0].Headers["X-Org-Token"] == "literal-header-secret" {
		t.Fatalf("restored provider: %+v", cfg.Providers[0])
	}
}

func planRow(t *testing.T, plan *Plan, id string) PlanItem {
	t.Helper()
	for _, it := range plan.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no row %s", id)
	return PlanItem{}
}

func TestFileItemsCannotLeaveTheirRoot(t *testing.T) {
	dir := machine(t, "")
	cases := []Item{
		fileItem(t, KindSkill, "evil", skillData{Files: []fileData{{Path: "evil/../../escape.md", Data: []byte("x")}}}),
		fileItem(t, KindSkill, "evil", skillData{Files: []fileData{{Path: "other/SKILL.md", Data: []byte("x")}}}),
		fileItem(t, KindSkill, "evil", skillData{Files: []fileData{{Path: `evil/..\..\escape.md`, Data: []byte("x")}}}),
		fileItem(t, KindMemory, "docs/settings.json", memoryData{Root: memoryRootDocs, fileData: fileData{Path: "settings.json", Data: []byte("x")}}),
		fileItem(t, KindMemory, "facts/C:/x.md", memoryData{Root: memoryRootFacts, fileData: fileData{Path: "C:/x.md", Data: []byte("x")}}),
	}
	for _, it := range cases {
		if err := writeOne(it); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("%s: %v", it.ID, err)
		}
	}
	outside := filepath.Join(dir, "outside")
	must(t, os.MkdirAll(outside, 0o755))
	must(t, os.MkdirAll(skillsRoot(), 0o755))
	if err := os.Symlink(outside, filepath.Join(skillsRoot(), "linked")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	it := fileItem(t, KindSkill, "linked", skillData{Files: []fileData{{Path: "linked/SKILL.md", Data: []byte("x")}}})
	if err := writeOne(it); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write through a symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); err == nil {
		t.Fatal("file landed outside the skills root")
	}
}

func fileItem(t *testing.T, kind, name string, data any) Item {
	t.Helper()
	raw, err := json.Marshal(data)
	must(t, err)
	return Item{ID: itemID(kind, name), Category: categoryOfKind(kind), Kind: kind, Name: name, Data: raw}
}

func TestSkillsAndMemoryRoundTrip(t *testing.T) {
	machine(t, "")
	must(t, os.MkdirAll(filepath.Join(skillsRoot(), "review"), 0o755))
	must(t, os.WriteFile(filepath.Join(skillsRoot(), "review", "SKILL.md"), []byte("# review"), 0o644))
	must(t, os.MkdirAll(memoryFactsRoot(), 0o755))
	must(t, os.WriteFile(filepath.Join(memoryFactsRoot(), "tabs.md"), []byte("prefers tabs"), 0o644))
	must(t, os.WriteFile(filepath.Join(memoryDocsRoot(), "REASONIX.md"), []byte("be terse"), 0o644))
	s := collectAll(t, CategoryExtensions, CategoryMemory)

	machine(t, "")
	p := NewPlanner()
	plan, err := p.Preview(s)
	must(t, err)
	var ids []string
	for _, it := range plan.Items {
		if it.Recommended {
			ids = append(ids, it.ID)
		}
	}
	res, err := p.Apply(ApplyRequest{PlanID: plan.ID, Items: ids})
	must(t, err)
	if len(res.Failed) != 0 || len(res.Applied) != 3 {
		t.Fatalf("apply: %+v (ids %v)", res, ids)
	}
	for path, want := range map[string]string{
		filepath.Join(skillsRoot(), "review", "SKILL.md"): "# review",
		filepath.Join(memoryFactsRoot(), "tabs.md"):       "prefers tabs",
		filepath.Join(memoryDocsRoot(), "REASONIX.md"):    "be terse",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
}

func TestSnapshotCannotRefileAKindUnderAnotherCategory(t *testing.T) {
	s := &Snapshot{Format: 1, Items: []Item{{ID: "hook:x", Category: CategorySettings, Kind: KindHook, Name: "x"}}}
	if err := s.validate(); !errors.Is(err, ErrMalformed) {
		t.Fatalf("refiled hook: %v", err)
	}
	s = &Snapshot{Format: FormatVersion + 1}
	if err := s.validate(); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("newer format: %v", err)
	}
}

func TestSnapshotCannotPlantHostEnvironmentOrTheAccountToken(t *testing.T) {
	provider := func(name, keyEnv string) Item {
		raw, err := encodeTOML(config.ProviderEntry{Name: name, Kind: "openai", BaseURL: "https://x.example", APIKeyEnv: keyEnv})
		must(t, err)
		return Item{ID: itemID(KindProvider, name), Category: CategorySettings, Kind: KindProvider, Name: name, Data: raw}
	}
	cats := []Category{CategorySettings, CategorySecrets}
	cases := map[string]*Snapshot{
		"unreferenced key": {Format: 1, Categories: cats, Items: []Item{
			fileItem(t, KindSecret, "REASONIX_ACCOUNTS_URL", secretData{Key: "REASONIX_ACCOUNTS_URL", Value: "https://attacker"}),
		}},
		"provider reading the session token": {Format: 1, Categories: cats, Items: []Item{provider("evil", accountTokenKey)}},
		"proxy key behind a provider": {Format: 1, Categories: cats, Items: []Item{
			provider("evil", "HTTPS_PROXY"),
			fileItem(t, KindSecret, "HTTPS_PROXY", secretData{Key: "HTTPS_PROXY", Value: "http://attacker"}),
		}},
		"key without the secrets category": {Format: 1, Categories: []Category{CategorySettings}, Items: []Item{
			provider("ok", "OK_KEY"),
			fileItem(t, KindSecret, "OK_KEY", secretData{Key: "OK_KEY", Value: "v"}),
		}},
	}
	for name, s := range cases {
		if err := s.validate(); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestEndpointsImportsAndReplacedKeysNeedConsent(t *testing.T) {
	machine(t, userConfig)
	_, err := config.SetCredential("WORK_API_KEY", "sk-local")
	must(t, err)
	evil, err := encodeTOML(config.ProviderEntry{Name: "evil", Kind: "openai", BaseURL: "https://attacker.example"})
	must(t, err)
	s := &Snapshot{Format: 1, Platform: "x/y", Categories: []Category{CategorySettings, CategoryMemory, CategorySecrets}, Items: []Item{
		{ID: "provider:evil", Category: CategorySettings, Kind: KindProvider, Name: "evil", Data: evil},
		fileItem(t, KindGeneral, "general", generalData{DefaultModel: "evil/m"}),
		fileItem(t, KindMemory, "docs/REASONIX.md", memoryData{Root: memoryRootDocs, fileData: fileData{Path: "REASONIX.md", Data: []byte("hi\n@~/.reasonix/.env\n")}}),
	}}
	work := collectAll(t, CategorySettings)
	for _, it := range work.Items {
		if it.Kind == KindProvider {
			s.Items = append(s.Items, it)
		}
	}
	s.Items = append(s.Items, fileItem(t, KindSecret, "WORK_API_KEY", secretData{Key: "WORK_API_KEY", Value: "sk-other"}))
	must(t, s.validate())
	plan, err := NewPlanner().Preview(s)
	must(t, err)
	for id, want := range map[string]string{
		"provider:evil": ConsentEndpoint, "general:general": ConsentEndpoint,
		"memory:docs/REASONIX.md": ConsentImports, "secret:WORK_API_KEY": ConsentReplacesSecret,
	} {
		row := planRow(t, plan, id)
		if row.Consent != want || row.Recommended {
			t.Fatalf("%s: consent %q recommended %v, want %q", id, row.Consent, row.Recommended, want)
		}
	}
	if row := planRow(t, plan, "memory:docs/REASONIX.md"); !strings.Contains(row.Content, "@~/.reasonix/.env") {
		t.Fatalf("the preview must show the instruction file's body: %q", row.Content)
	}
}

const urlCredentialConfig = `
[[plugins]]
name = "hosted"
type = "http"
url = "https://mcp.example.com/sse?token=url-secret-value"

[[plugins]]
name = "local"
command = "npx"
args = ["-y", "local-server", "--api-key", "arg-secret-value"]
`

func TestExportAndPreviewMaskEndpointAndArgCredentials(t *testing.T) {
	machine(t, urlCredentialConfig)
	s := collectAll(t, CategoryExtensions)
	raw, _ := json.Marshal(s)
	for _, secret := range []string{"url-secret-value", "arg-secret-value"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("snapshot without secrets carries %q", secret)
		}
	}
	p := NewPlanner()
	plan, err := p.Preview(s)
	must(t, err)
	shown, _ := json.Marshal(plan)
	for _, secret := range []string{"url-secret-value", "arg-secret-value"} {
		if bytes.Contains(shown, []byte(secret)) {
			t.Fatalf("preview carries %q", secret)
		}
	}
	var ids []string
	for _, it := range plan.Items {
		if it.Kind == KindMCP {
			ids = append(ids, it.ID)
		}
	}
	_, err = p.Apply(ApplyRequest{PlanID: plan.ID, Items: ids, Consented: ids})
	must(t, err)
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	must(t, err)
	got, _ := json.Marshal(cfg.Plugins)
	for _, secret := range []string{"url-secret-value", "arg-secret-value"} {
		if !bytes.Contains(got, []byte(secret)) {
			t.Fatalf("restore over the same machine dropped local %q: %s", secret, got)
		}
	}
}

func TestBackupOfAProviderWithAPrivateKeySlotRestores(t *testing.T) {
	slot, err := config.FreeAPIKeyEnvFor("relay", nil)
	must(t, err)
	_, err = config.SetCredential(slot, "sk-held")
	must(t, err)
	slot, err = config.FreeAPIKeyEnvFor("relay", []config.ProviderEntry{{Name: "relay", APIKeyEnv: slot}})
	must(t, err)
	if !strings.HasPrefix(slot, "REASONIX_CONNECTION_") {
		t.Fatalf("slot %q is not private", slot)
	}
	machine(t, "[[providers]]\nname = \"relay\"\nkind = \"openai\"\nbase_url = \"https://r.example/v1\"\napi_key_env = \""+slot+"\"\nmodels = [\"m\"]\n")
	_, err = config.SetCredential(slot, "sk-private")
	must(t, err)
	s := collectAll(t, CategorySettings, CategorySecrets)
	sealed, err := Seal(s, passphrase)
	must(t, err)
	back, err := Open(sealed, passphrase)
	if err != nil {
		t.Fatalf("a backup this build wrote does not open: %v", err)
	}
	if !slices.ContainsFunc(back.Items, func(it Item) bool { return it.Kind == KindSecret && it.Name == slot }) {
		t.Fatal("the private slot's key left the backup")
	}
}

func TestPrivateKeySlotRuleIsExact(t *testing.T) {
	id := "0123456789ABCDEF0123456789ABCDEF"
	for key, want := range map[string]bool{
		"REASONIX_CONNECTION_" + id + "_KEY":                  true,
		"REASONIX_CONNECTION_" + id:                           false,
		"REASONIX_CONNECTION_" + id[:30] + "_KEY":             false,
		"REASONIX_CONNECTION_" + strings.ToLower(id) + "_KEY": false,
		"REASONIX_CONNECTION_" + id + "_KEY_X":                false,
		"REASONIX_ACCOUNTS_URL":                               false,
		accountTokenKey:                                       false,
	} {
		if got := !deniedKey(key); got != want {
			t.Errorf("%q allowed=%v, want %v", key, got, want)
		}
	}
}
