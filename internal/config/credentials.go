package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"reasonix/internal/fileutil"
)

const (
	CredentialsStoreAuto    = "auto"
	CredentialsStoreKeyring = "keyring"
	CredentialsStoreFile    = "file"

	credentialsKeyringService = "reasonix"
)

const (
	CredentialSourceEnvironment = "environment"
	CredentialSourceProjectEnv  = "project_env"
	CredentialSourceCredentials = "credentials"
	CredentialSourceHomeEnv     = "home_env"
	CredentialSourceLegacy      = "legacy_credentials"
)

type CredentialSource struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Label string `json:"label,omitempty"`
}

type CredentialResolution struct {
	Name     string             `json:"name"`
	Set      bool               `json:"set"`
	Value    string             `json:"-"`
	Source   CredentialSource   `json:"source,omitempty"`
	Shadowed []CredentialSource `json:"shadowed,omitempty"`
}

type trackedCredentialSource struct {
	source CredentialSource
	value  string
}

var credentialSourceTracker = struct {
	sync.Mutex
	byKey map[string]trackedCredentialSource
}{byKey: map[string]trackedCredentialSource{}}

var storedCredentialValueLookup = storedCredentialValue
var legacyKeyringCredentialValueLookup = legacyKeyringCredentialValue

// CredentialResolver resolves credentials repeatedly for one caller-owned view
// build. It keeps expensive global credential-store lookups bounded to one per
// key while preserving the same source/shadow reporting as the one-shot helpers.
type CredentialResolver struct {
	root string

	mu               sync.Mutex
	globalFirstCache map[string]CredentialResolution
}

// NewCredentialResolverForRoot returns a resolver scoped to a workspace root.
func NewCredentialResolverForRoot(root string) *CredentialResolver {
	return &CredentialResolver{root: resolveRoot(root)}
}

// ResolveGlobalFirst resolves key from Reasonix's global .env only. Repeated
// calls for the same key reuse the first result so UI views with multiple
// provider entries sharing api_key_env stay consistent.
func (r *CredentialResolver) ResolveGlobalFirst(key string) CredentialResolution {
	key = strings.TrimSpace(key)
	if key == "" {
		return CredentialResolution{Name: key}
	}
	if r == nil {
		return resolveCredentialForRootGlobalFirst(".", key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.globalFirstCache == nil {
		r.globalFirstCache = map[string]CredentialResolution{}
	}
	if cached, ok := r.globalFirstCache[key]; ok {
		return cloneCredentialResolution(cached)
	}
	res := resolveCredentialForRootGlobalFirst(r.root, key)
	r.globalFirstCache[key] = cloneCredentialResolution(res)
	return res
}

func cloneCredentialResolution(res CredentialResolution) CredentialResolution {
	if len(res.Shadowed) > 0 {
		res.Shadowed = append([]CredentialSource(nil), res.Shadowed...)
	}
	return res
}

func normalizeCredentialsStore(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case CredentialsStoreKeyring:
		return CredentialsStoreKeyring
	case CredentialsStoreFile:
		return CredentialsStoreFile
	default:
		return CredentialsStoreAuto
	}
}

func credentialsStoreMode() string {
	if mode := strings.TrimSpace(os.Getenv("REASONIX_CREDENTIALS_STORE")); mode != "" {
		return normalizeCredentialsStore(mode)
	}
	var partial struct {
		CredentialsStore string `toml:"credentials_store"`
	}
	if path := userConfigLoadPath(); path != "" {
		_, _ = toml.DecodeFile(path, &partial)
	}
	return normalizeCredentialsStore(partial.CredentialsStore)
}

func credentialEnvNamesForRoot(root string) []string {
	root = resolveRoot(root)
	cfg := Default()

	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}
	if uc := userConfigLoadPath(); uc != "" {
		_ = mergeFile(cfg, uc)
	}
	_ = mergeFile(cfg, projectTOML)
	var tomlSources []string
	if uc := userConfigLoadPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
	}
	tomlSources = append(tomlSources, projectTOML)
	if providers, _, _, ok, err := mergeTOMLProviders(tomlSources); err == nil && ok {
		cfg.Providers = providers
	}

	return credentialEnvNamesFromConfig(cfg)
}

func credentialEnvNamesFromConfig(cfg *Config) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, p := range cfg.Providers {
		add(p.APIKeyEnv)
	}
	add(cfg.Bot.QQ.AppSecretEnv)
	add(cfg.Bot.Feishu.AppSecretEnv)
	add(cfg.Bot.Weixin.TokenEnv)
	for _, conn := range cfg.Bot.Connections {
		add(conn.Credential.AppSecretEnv)
		add(conn.Credential.TokenEnv)
	}
	sort.Strings(out)
	return out
}

func resolveProviderCredentialsForRoot(root string, cfg *Config) {
	if cfg == nil || len(cfg.Providers) == 0 {
		return
	}
	resolver := NewCredentialResolverForRoot(root)
	for i := range cfg.Providers {
		resolveProviderCredentialWithResolver(&cfg.Providers[i], resolver)
	}
}

func resolveProviderCredentialWithResolver(entry *ProviderEntry, resolver *CredentialResolver) {
	if entry == nil {
		return
	}
	key := strings.TrimSpace(entry.APIKeyEnv)
	if key == "" {
		entry.resolvedAPIKey = ""
		entry.resolvedSource = CredentialSource{}
		return
	}
	if resolver == nil {
		resolver = NewCredentialResolverForRoot(".")
	}
	res := resolver.ResolveGlobalFirst(key)
	if !res.Set || res.Value == "" {
		entry.resolvedAPIKey = ""
		entry.resolvedSource = CredentialSource{}
		return
	}
	entry.resolvedAPIKey = res.Value
	entry.resolvedSource = res.Source
}

func (e *ProviderEntry) ResolveAPIKeyForRoot(root string) {
	resolveProviderCredentialWithResolver(e, NewCredentialResolverForRoot(root))
}

func loadCredentialStoreForRoot(root string) {
	names := credentialEnvNamesForRoot(root)
	if len(names) == 0 {
		return
	}
	if p := UserCredentialsPath(); p != "" {
		loadDotEnvFileAs(p, CredentialSource{Kind: CredentialSourceCredentials, Path: p, Label: "Reasonix credentials (.env)"})
	}
}

// StoreCredentialLines stores KEY=value assignments in Reasonix's global .env
// and pins them into the current process environment.
func StoreCredentialLines(lines []string) (string, error) {
	assignments := parseCredentialLines(lines)
	if len(assignments) == 0 {
		return CredentialsTargetDescription(), nil
	}
	if err := storeCredentialsInFile(UserCredentialsPath(), assignments); err != nil {
		return "", err
	}
	pinCredentialAssignments(assignments)
	return UserCredentialsPath(), nil
}

func SetCredential(key, value string) (string, error) {
	key = strings.TrimSpace(key)
	if !isCredentialKey(key) {
		return "", fmt.Errorf("invalid credential key %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("credential value for %s contains a newline", key)
	}
	return StoreCredentialLines([]string{key + "=" + value})
}

func RemoveCredential(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if path := UserCredentialsPath(); path != "" {
		if err := removeCredentialFromFile(path, key); err != nil {
			return err
		}
	}
	return os.Unsetenv(key)
}

func CredentialIsSet(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return CredentialStored(key)
}

func CredentialStored(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return envFileHasKey(UserCredentialsPath(), key)
}

func credentialCurrentStoreHasKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return envFileHasKey(UserCredentialsPath(), key)
}

func CredentialsTargetDescription() string {
	return UserCredentialsPath()
}

func parseCredentialLines(lines []string) map[string]string {
	out := map[string]string{}
	for _, raw := range lines {
		line := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if !ok || !isCredentialKey(key) || strings.ContainsAny(value, "\r\n") {
			continue
		}
		out[key] = value
	}
	return out
}

func pinCredentialAssignments(assignments map[string]string) {
	for key, value := range assignments {
		_ = os.Setenv(key, value)
		recordCredentialSource(key, value, CredentialSource{Kind: CredentialSourceCredentials, Path: UserCredentialsPath(), Label: "Reasonix credentials (.env)"})
	}
}

func recordExistingCredentialSource(key string) {
	key = strings.TrimSpace(key)
	value := os.Getenv(key)
	if key == "" || value == "" {
		return
	}
	credentialSourceTracker.Lock()
	defer credentialSourceTracker.Unlock()
	if current, ok := credentialSourceTracker.byKey[key]; ok && current.value == value {
		return
	}
	if _, ok := credentialSourceTracker.byKey[key]; ok {
		return
	}
	credentialSourceTracker.byKey[key] = trackedCredentialSource{
		source: CredentialSource{Kind: CredentialSourceEnvironment, Label: "environment variable"},
		value:  value,
	}
}

func recordCredentialSource(key, value string, source CredentialSource) {
	key = strings.TrimSpace(key)
	if key == "" || value == "" {
		return
	}
	source.Label = credentialSourceLabel(source)
	credentialSourceTracker.Lock()
	credentialSourceTracker.byKey[key] = trackedCredentialSource{source: source, value: value}
	credentialSourceTracker.Unlock()
}

func trackedCredential(key, value string) (CredentialSource, bool) {
	credentialSourceTracker.Lock()
	defer credentialSourceTracker.Unlock()
	current, ok := credentialSourceTracker.byKey[key]
	if !ok || current.value != value {
		return CredentialSource{}, false
	}
	return current.source, true
}

func credentialSourceLabel(source CredentialSource) string {
	if strings.TrimSpace(source.Label) != "" {
		return source.Label
	}
	switch source.Kind {
	case CredentialSourceProjectEnv:
		return "project .env"
	case CredentialSourceCredentials:
		return "Reasonix credentials"
	case CredentialSourceHomeEnv:
		return "home .env"
	case CredentialSourceLegacy:
		return "legacy Reasonix credentials"
	case CredentialSourceEnvironment:
		return "environment variable"
	default:
		return ""
	}
}

func ResolveCredential(key string) CredentialResolution {
	return ResolveCredentialForRoot(".", key)
}

func ResolveCredentialForRoot(root, key string) CredentialResolution {
	key = strings.TrimSpace(key)
	res := CredentialResolution{Name: key}
	if key == "" {
		return res
	}
	value := os.Getenv(key)
	if value == "" {
		return res
	}
	res.Set = true
	res.Value = value
	if source, ok := trackedCredential(key, value); ok {
		res.Source = source
	} else if source, ok := inferCredentialSource(root, key, value); ok {
		res.Source = source
	} else {
		res.Source = CredentialSource{Kind: CredentialSourceEnvironment, Label: credentialSourceLabel(CredentialSource{Kind: CredentialSourceEnvironment})}
	}
	res.Source.Label = credentialSourceLabel(res.Source)
	res.Shadowed = shadowedCredentialSources(root, key, value, res.Source)
	return res
}

func ResolveCredentialForRootGlobalFirst(root, key string) CredentialResolution {
	key = strings.TrimSpace(key)
	return NewCredentialResolverForRoot(root).ResolveGlobalFirst(key)
}

func resolveCredentialForRootGlobalFirst(root, key string) CredentialResolution {
	root = resolveRoot(root)
	res := CredentialResolution{Name: key}
	if key == "" {
		return res
	}
	if value, source, ok := storedCredentialValueLookup(key); ok {
		res.Set = true
		res.Value = value
		res.Source = source
		res.Source.Label = credentialSourceLabel(res.Source)
		res.Shadowed = shadowedCredentialSources(root, key, value, res.Source)
		return res
	}
	return res
}

func storedCredentialValue(key string) (string, CredentialSource, bool) {
	if p := UserCredentialsPath(); p != "" {
		if value, ok := envFileValue(p, key); ok && value != "" {
			return value, CredentialSource{Kind: CredentialSourceCredentials, Path: p, Label: "Reasonix credentials (.env)"}, true
		}
	}
	return "", CredentialSource{}, false
}

func inferCredentialSource(root, key, value string) (CredentialSource, bool) {
	for _, candidate := range credentialSourceCandidates(root) {
		if v, ok := envFileValue(candidate.Path, key); ok && v == value {
			candidate.Label = credentialSourceLabel(candidate)
			return candidate, true
		}
	}
	return CredentialSource{}, false
}

func shadowedCredentialSources(root, key, activeValue string, active CredentialSource) []CredentialSource {
	var out []CredentialSource
	for _, candidate := range credentialSourceCandidates(root) {
		if sameCredentialSource(candidate, active) {
			continue
		}
		if v, ok := envFileValue(candidate.Path, key); ok && v != activeValue {
			candidate.Label = credentialSourceLabel(candidate)
			out = append(out, candidate)
		}
	}
	return out
}

func credentialSourceCandidates(root string) []CredentialSource {
	root = resolveRoot(root)
	var out []CredentialSource
	dotEnvPath := ".env"
	if root != "" && root != "." {
		dotEnvPath = filepath.Join(root, ".env")
	}
	out = append(out, CredentialSource{Kind: CredentialSourceProjectEnv, Path: dotEnvPath})
	if p := UserCredentialsPath(); p != "" {
		out = append(out, CredentialSource{Kind: CredentialSourceCredentials, Path: p})
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, CredentialSource{Kind: CredentialSourceHomeEnv, Path: filepath.Join(home, ".env")})
	}
	return out
}

func sameCredentialSource(a, b CredentialSource) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Path == "" || b.Path == "" {
		return a.Path == b.Path
	}
	return samePath(a.Path, b.Path)
}

func storeCredentialsInFile(path string, assignments map[string]string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("credentials store unavailable")
	}
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return err
	}
	replaced := map[string]bool{}
	for i, line := range lines {
		key, ok := credentialLineKey(line)
		if !ok {
			continue
		}
		if value, hit := assignments[key]; hit {
			lines[i] = key + "=" + value
			replaced[key] = true
		}
	}
	keys := make([]string, 0, len(assignments))
	for key := range assignments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !replaced[key] {
			lines = append(lines, key+"="+assignments[key])
		}
	}
	return writeCredentialFileLines(path, lines)
}

func removeCredentialFromFile(path, key string) error {
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if k, ok := credentialLineKey(line); ok && k == key {
			continue
		}
		out = append(out, line)
	}
	return writeCredentialFileLines(path, out)
}

func readCredentialFileLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeCredentialFileLines(path string, lines []string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("credentials store unavailable")
	}
	out := ""
	if len(lines) > 0 {
		out = strings.Join(lines, "\n") + "\n"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func credentialLineKey(line string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(line), "export ")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	key, _, ok := strings.Cut(trimmed, "=")
	key = strings.TrimSpace(key)
	return key, ok && isCredentialKey(key)
}

func isCredentialKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func envFileHasKey(path, key string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return false
	}
	for _, line := range lines {
		if k, ok := credentialLineKey(line); ok && k == key {
			return true
		}
	}
	return false
}
