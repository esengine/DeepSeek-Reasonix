package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

type DiagnosticFinding struct {
	Severity    string `json:"severity"` // error | warning | info
	Code        string `json:"code"`
	Scope       string `json:"scope,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type DiagnosticReport struct {
	GeneratedAt   string               `json:"generatedAt"`
	Root          string               `json:"root"`
	Network       bool                 `json:"network"`
	Snapshots     []DiagnosticSnapshot `json:"snapshots"`
	PendingUpdate *DiagnosticUpdate    `json:"pendingUpdate,omitempty"`
	Findings      []DiagnosticFinding  `json:"findings"`
}

type DiagnosticSnapshot struct {
	ID         string `json:"id"`
	RecordedAt string `json:"recordedAt"`
	Version    string `json:"version,omitempty"`
}

type DiagnosticUpdate struct {
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion"`
}

type DiagnoseOptions struct {
	Root    string
	Network bool
	Timeout time.Duration
}

func Diagnose(ctx context.Context, opts DiagnoseOptions) (DiagnosticReport, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	report := DiagnosticReport{GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Root: root, Network: opts.Network, Snapshots: []DiagnosticSnapshot{}, Findings: []DiagnosticFinding{}}
	if snapshots, listErr := ListConfigSnapshots(); listErr == nil {
		for _, snapshot := range snapshots {
			report.Snapshots = append(report.Snapshots, DiagnosticSnapshot{ID: snapshot.ID, RecordedAt: snapshot.RecordedAt, Version: snapshot.Version})
		}
	}
	if pending, pendingErr := ReadPendingUpdate(); pendingErr == nil {
		report.PendingUpdate = &DiagnosticUpdate{FromVersion: pending.FromVersion, ToVersion: pending.ToVersion}
	}
	configReport, err := InspectAndRepairConfig(ConfigOptions{Root: root})
	if err != nil {
		return report, err
	}
	valid := true
	for _, check := range configReport.Checks {
		if check.Exists && !check.Valid {
			valid = false
			report.add("error", "config.invalid_toml", check.Scope, "Configuration cannot be parsed: "+check.Error, "Run reasonix-guard repair; add --project only for a project config.")
		}
	}
	checkSensitiveFileMode(&report, config.UserConfigPath(), "global config")
	checkSensitiveFileMode(&report, config.UserCredentialsPath(), "credential file")
	checkDirectoryMode(&report, config.ReasonixHomeDir(), "Reasonix home")
	if config.MemoryUserDir() != config.ReasonixHomeDir() {
		checkDirectoryMode(&report, config.MemoryUserDir(), "Reasonix state directory")
	}
	checkDirectoryMode(&report, root, "project root")
	checkDerivedJSON(&report)
	if !valid {
		return report, nil
	}
	// Diagnose is documented as read-only: LoadForRoot would rewrite legacy MCP
	// `tier` lines on disk, so use the variant that never writes config files.
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		report.add("error", "config.load_failed", "runtime", err.Error(), "Start in Safe Mode, then inspect global and project configuration.")
		return report, nil
	}
	validateProviders(&report, cfg)
	validatePlugins(&report, cfg, root)
	validatePermissions(&report, cfg)
	if err := netclient.Validate(cfg.NetworkProxySpec()); err != nil {
		report.add("error", "network.invalid_proxy", "network", "Proxy configuration is invalid: "+err.Error(), "Correct [network] proxy settings or set proxy_mode = \"off\".")
	}
	if opts.Network {
		timeout := opts.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		probeProviderNetwork(ctx, &report, cfg, timeout)
	}
	return report, nil
}

func (r *DiagnosticReport) add(severity, code, scope, message, remediation string) {
	r.Findings = append(r.Findings, DiagnosticFinding{Severity: severity, Code: code, Scope: scope, Message: message, Remediation: remediation})
}

func (r DiagnosticReport) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == "error" {
			return true
		}
	}
	return false
}

func validateProviders(report *DiagnosticReport, cfg *config.Config) {
	seen := map[string]bool{}
	for i := range cfg.Providers {
		entry := &cfg.Providers[i]
		scope := "provider:" + strings.TrimSpace(entry.Name)
		if strings.TrimSpace(entry.Name) == "" {
			report.add("error", "provider.missing_name", "provider", "A provider has no name.", "Set a stable, unique provider name.")
			continue
		}
		if seen[entry.Name] {
			report.add("error", "provider.duplicate_name", scope, "Provider name is declared more than once.", "Rename or remove the duplicate provider entry.")
		}
		seen[entry.Name] = true
		if entry.Kind != "openai" && entry.Kind != "anthropic" {
			report.add("error", "provider.unsupported_kind", scope, fmt.Sprintf("Provider kind %q is not registered by packaged Reasonix builds.", entry.Kind), "Use openai or anthropic compatibility.")
		}
		if len(entry.ModelList()) == 0 {
			report.add("error", "provider.no_models", scope, "Provider has no configured model.", "Set model or models.")
		}
		if err := validateHTTPURL(entry.BaseURL); err != nil {
			report.add("error", "provider.invalid_url", scope, "Provider base URL is invalid: "+err.Error(), "Use an absolute http:// or https:// URL.")
		}
		if entry.ModelsURL != "" {
			if err := validateHTTPURL(entry.ModelsURL); err != nil {
				report.add("error", "provider.invalid_models_url", scope, "Provider models URL is invalid: "+err.Error(), "Use an absolute http:// or https:// URL.")
			}
		}
		if entry.APIKeyEnv != "" && !config.IsValidCredentialKey(entry.APIKeyEnv) {
			report.add("error", "provider.invalid_key_name", scope, "api_key_env is not a valid environment variable name.", "Use letters, numbers, and underscores.")
		} else if entry.RequiresAPIKey() && entry.APIKey() == "" {
			report.add("warning", "provider.missing_key", scope, "The configured API key is missing from the global Reasonix credential file.", "Add the key in Settings or <Reasonix home>/.env.")
		}
	}
	if strings.TrimSpace(cfg.DefaultModel) == "" {
		report.add("warning", "model.no_default", "model", "No default model is configured.", "Select a default provider/model in Settings.")
	} else if _, ok := cfg.ResolveModel(cfg.DefaultModel); !ok {
		report.add("error", "model.invalid_default", "model", fmt.Sprintf("Default model %q does not resolve to a configured provider/model.", cfg.DefaultModel), "Choose an existing provider or provider/model reference.")
	}
}

func validatePlugins(report *DiagnosticReport, cfg *config.Config, root string) {
	seen := map[string]bool{}
	for _, plugin := range cfg.Plugins {
		scope := "plugin:" + strings.TrimSpace(plugin.Name)
		if plugin.Name == "" {
			report.add("error", "plugin.missing_name", "plugin", "An MCP server has no name.", "Set a stable MCP server name.")
			continue
		}
		if seen[plugin.Name] {
			report.add("error", "plugin.duplicate_name", scope, "MCP server name is declared more than once.", "Remove or rename the duplicate entry.")
		}
		seen[plugin.Name] = true
		switch strings.ToLower(strings.TrimSpace(plugin.Type)) {
		case "", "stdio":
			command := strings.TrimSpace(plugin.Command)
			if command == "" {
				report.add("error", "plugin.missing_command", scope, "stdio MCP server has no command.", "Configure an executable command or disable the server.")
			} else if !commandAvailable(command, root) {
				report.add("warning", "plugin.command_missing", scope, fmt.Sprintf("MCP command %q was not found.", command), "Install the command, use an absolute path, or disable auto_start.")
			}
		case "http", "sse", "streamable-http":
			if err := validateHTTPURL(plugin.URL); err != nil {
				report.add("error", "plugin.invalid_url", scope, "Remote MCP URL is invalid: "+err.Error(), "Use an absolute http:// or https:// URL.")
			}
		default:
			report.add("error", "plugin.invalid_type", scope, fmt.Sprintf("Unknown MCP transport %q.", plugin.Type), "Use stdio, http, or sse.")
		}
	}
}

func validatePermissions(report *DiagnosticReport, cfg *config.Config) {
	lists := map[string][]string{"allow": cfg.Permissions.Allow, "ask": cfg.Permissions.Ask, "deny": cfg.Permissions.Deny}
	owners := map[string][]string{}
	for name, rules := range lists {
		for _, rule := range rules {
			rule = strings.TrimSpace(rule)
			if rule != "" {
				owners[rule] = append(owners[rule], name)
			}
		}
	}
	rules := make([]string, 0, len(owners))
	for rule := range owners {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	for _, rule := range rules {
		if len(owners[rule]) > 1 {
			report.add("warning", "permissions.conflict", "permissions", fmt.Sprintf("Permission rule %q appears in %s; deny takes precedence.", rule, strings.Join(owners[rule], ", ")), "Keep each exact rule in one permission list.")
		}
	}
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func commandAvailable(command, root string) bool {
	if strings.ContainsAny(command, `/\\`) || filepath.IsAbs(command) {
		if !filepath.IsAbs(command) {
			command = filepath.Join(root, command)
		}
		st, err := os.Stat(command)
		if err != nil || st.IsDir() {
			return false
		}
		return runtime.GOOS == "windows" || st.Mode().Perm()&0o111 != 0
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func checkSensitiveFileMode(report *DiagnosticReport, path, label string) {
	if runtime.GOOS == "windows" || path == "" {
		return
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return
	}
	if st.Mode().Perm()&0o077 != 0 {
		report.add("warning", "file.permissions", label, fmt.Sprintf("%s is readable by group or other users.", label), "Set file permissions to 0600.")
	}
}

func checkDirectoryMode(report *DiagnosticReport, path, label string) {
	if path == "" {
		report.add("warning", "directory.unavailable", label, label+" path is unavailable.", "Restore the user home or Reasonix path environment configuration.")
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			report.add("warning", "directory.unreadable", label, label+" cannot be inspected.", "Check directory ownership and access permissions.")
		}
		return
	}
	if !st.IsDir() {
		report.add("error", "directory.not_directory", label, label+" is not a directory.", "Correct the configured path.")
		return
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o200 == 0 {
		report.add("warning", "directory.not_writable", label, label+" is not owner-writable.", "Grant the current user write access or choose another path.")
	}
}

func checkDerivedJSON(report *DiagnosticReport) {
	paths := derivedStatePaths()
	for name, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !json.Valid(b) {
			report.add("warning", "derived.invalid_json", "derived:"+name, fmt.Sprintf("Derived desktop state %s is malformed.", filepath.Base(path)), "Run reasonix-guard rebuild --target "+name+".")
		}
	}
}

func probeProviderNetwork(ctx context.Context, report *DiagnosticReport, cfg *config.Config, timeout time.Duration) {
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{DialTimeout: timeout, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout})
	if err != nil {
		report.add("error", "network.client_failed", "network", "Cannot build network client: "+err.Error(), "Correct proxy settings.")
		return
	}
	client.Timeout = timeout
	for i := range cfg.Providers {
		entry := &cfg.Providers[i]
		if validateHTTPURL(entry.BaseURL) != nil {
			continue
		}
		urls, err := config.BuildModelFetchURLs(entry.BaseURL, entry.ModelsURL)
		if err != nil || len(urls) == 0 {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, urls[0], nil)
		if err == nil {
			for key, value := range entry.Headers {
				req.Header.Set(key, value)
			}
			if key := entry.APIKey(); key != "" {
				if entry.Kind == "anthropic" && !entry.AuthHeader {
					req.Header.Set("x-api-key", key)
				} else {
					req.Header.Set("Authorization", "Bearer "+key)
				}
			}
			resp, callErr := client.Do(req)
			if callErr != nil {
				report.add("warning", "network.unreachable", "provider:"+entry.Name, "Provider endpoint could not be reached: "+redactNetworkError(callErr), "Check DNS, proxy, firewall, and provider availability.")
			} else {
				_ = resp.Body.Close()
				switch {
				case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
					report.add("error", "network.authentication_failed", "provider:"+entry.Name, fmt.Sprintf("Provider rejected credentials with HTTP %d.", resp.StatusCode), "Update the provider credential in Reasonix Settings.")
				case resp.StatusCode >= 200 && resp.StatusCode < 300:
					report.add("info", "network.ok", "provider:"+entry.Name, "Provider endpoint and credentials are reachable.", "")
				default:
					report.add("warning", "network.unexpected_status", "provider:"+entry.Name, fmt.Sprintf("Provider model endpoint returned HTTP %d.", resp.StatusCode), "Verify models_url or test the provider from Settings.")
				}
			}
		}
		cancel()
	}
}

func redactNetworkError(err error) string {
	if err == nil {
		return ""
	}
	return "network request failed"
}
