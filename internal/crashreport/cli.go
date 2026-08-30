// Package crashreport owns local, user-reviewed CLI crash reports.
package crashreport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/fileutil"
	"reasonix/internal/netclient"
)

const (
	dirName              = "cli-crash-reports"
	currentSchemaVersion = 2
	maxReports           = 10
	maxMessageBytes      = 16 << 10
	maxStackBytes        = 8 << 10
	maxFieldBytes        = 4 << 10
)

var reportEndpoint = "https://crash.reasonix.io/v1/report"

var queueMu sync.Mutex

var (
	userPathSegment       = regexp.MustCompile(`(?i)([A-Z]:\\Users\\|/(?:home|Users)/)[^/\\:\s"']+`)
	emailPattern          = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	secretKeyValuePattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|authorization|secret|password|passwd|pwd|token)\b\s*[:=]\s*(?:Bearer\s+)?['"]?[^'"\s,;]+['"]?`)
	bearerTokenPattern    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`)
	explicitKeyPattern    = regexp.MustCompile(`\b(?:sk|rk)-(?:proj-)?[A-Za-z0-9_-]{16,}\b`)
	envIdentifierPattern  = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*(?:API[_-]?KEY|ACCESS[_-]?KEY|PRIVATE[_-]?KEY|SECRET|TOKEN|PASSWORD|PASSWD|PWD)[A-Z0-9_]*\b`)
	jwtPattern            = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	longHexPattern        = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	longBase64Pattern     = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	longBase64URLPattern  = regexp.MustCompile(`\b[A-Za-z0-9_-]{48,}\b`)
	goLocationPattern     = regexp.MustCompile(`[^\s]+\.go:\d+`)
	reportFilenamePattern = regexp.MustCompile(`^[0-9]{20}-[0-9]+-[0-9a-f]{16}\.json$`)
	fingerprintNumber     = regexp.MustCompile(`\b\d+\b`)
)

// Report is the subset of the shared crash ingest protocol emitted by the CLI.
type Report struct {
	EventID       string `json:"eventId,omitempty"`
	DedupKey      string `json:"dedupKey,omitempty"`
	Kind          string `json:"kind"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Message       string `json:"message"`
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Source        string `json:"source,omitempty"`
	Label         string `json:"label,omitempty"`
	ErrorType     string `json:"errorType,omitempty"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
	Stack         string `json:"stack,omitempty"`
	TopFrame      string `json:"topFrame,omitempty"`
	OccurredAt    string `json:"occurredAt,omitempty"`
}

// Pending is a locally stored report. ID is safe to pass back to Load or Remove.
type Pending struct {
	ID     string
	Report Report
}

var ErrNoReports = errors.New("no pending CLI crash reports")

// CapturePanic records a sanitized panic report locally. The panic value itself
// is deliberately never serialized because it can contain prompts, commands,
// paths, or provider response content.
func CapturePanic(home, version string, recovered any, stack []byte) error {
	if strings.TrimSpace(home) == "" {
		return errors.New("crash report: empty Reasonix home")
	}
	cleanStack := sanitizeStack(string(stack))
	report := Report{
		Kind:          "crash",
		Version:       sanitizeField(defaultString(version, "unknown"), 64),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Message:       "[cli panic]\n\nUnhandled CLI panic.",
		SchemaVersion: currentSchemaVersion,
		Source:        "cli.go",
		Label:         "panic",
		ErrorType:     sanitizeField(fmt.Sprintf("%T", recovered), 128),
		ErrorMessage:  "Unhandled CLI panic.",
		Stack:         cleanStack,
		TopFrame:      topFrame(cleanStack),
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	return write(home, report)
}

// List returns valid local reports newest first. Unknown or malformed files are
// ignored but retained so an older binary never destroys a newer report format.
func List(home string) ([]Pending, error) {
	dir := filepath.Join(home, dirName)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Pending, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var report Report
		if json.Unmarshal(body, &report) != nil || !valid(report) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		identityChanged := report.EventID == "" || report.DedupKey == ""
		ensureReportIdentity(&report, id)
		report = sanitizeReport(report)
		if identityChanged {
			if updated, marshalErr := json.Marshal(report); marshalErr == nil {
				_ = fileutil.AtomicWriteFile(filepath.Join(dir, entry.Name()), updated, 0o600)
			}
		}
		out = append(out, Pending{ID: id, Report: report})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Load returns one report by ID, or the newest report when ID is empty.
func Load(home, id string) (Pending, error) {
	reports, err := List(home)
	if err != nil {
		return Pending{}, err
	}
	if len(reports) == 0 {
		return Pending{}, ErrNoReports
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return reports[0], nil
	}
	for _, report := range reports {
		if report.ID == id {
			return report, nil
		}
	}
	return Pending{}, fmt.Errorf("CLI crash report %q not found", id)
}

// Preview returns the sanitized report in readable JSON form.
func Preview(report Report) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(sanitizeReport(report)); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// Send uploads a single user-reviewed report. It does not remove local state;
// callers remove the report only after a successful response.
func Send(ctx context.Context, report Report, proxy netclient.ProxySpec) error {
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{
		DialTimeout:           3 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	client.Timeout = 10 * time.Second
	return sendWithClient(ctx, client, reportEndpoint, report)
}

func sendWithClient(ctx context.Context, client *http.Client, endpoint string, report Report) error {
	body, err := json.Marshal(sanitizeReport(report))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("crash endpoint returned %s", resp.Status)
	}
	return nil
}

// Remove deletes one report previously returned by List or Load.
func Remove(home, id string) error {
	report, err := Load(home, id)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(home, dirName, report.ID+".json"))
}

func write(home string, report Report) error {
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	id := fmt.Sprintf("%020d-%d-%s", time.Now().UTC().UnixNano(), os.Getpid(), hex.EncodeToString(nonce))
	ensureReportIdentity(&report, id)
	body, err := json.Marshal(sanitizeReport(report))
	if err != nil {
		return err
	}
	queueMu.Lock()
	defer queueMu.Unlock()
	if err := fileutil.AtomicWriteFile(filepath.Join(dir, id+".json"), body, 0o600); err != nil {
		return err
	}
	prune(dir)
	return nil
}

func ensureReportIdentity(report *Report, stableID string) {
	if report.EventID == "" {
		sum := sha256.Sum256([]byte("reasonix-cli-event\n" + stableID))
		report.EventID = hex.EncodeToString(sum[:16])
	}
	if report.DedupKey == "" {
		basis := strings.Join([]string{
			report.Kind,
			report.Version,
			report.Source,
			report.Label,
			report.ErrorType,
			normalizeFingerprintField(report.ErrorMessage),
			normalizeFingerprintField(report.TopFrame),
		}, "\n")
		sum := sha256.Sum256([]byte(basis))
		report.DedupKey = hex.EncodeToString(sum[:])
	}
}

func normalizeFingerprintField(value string) string {
	return fingerprintNumber.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "<n>")
}

func prune(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !reportFilenamePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var report Report
		if json.Unmarshal(body, &report) != nil || !valid(report) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for len(paths) > maxReports {
		_ = os.Remove(paths[0])
		paths = paths[1:]
	}
}

func valid(report Report) bool {
	return report.SchemaVersion >= 0 && report.SchemaVersion <= currentSchemaVersion &&
		report.Kind == "crash" && strings.TrimSpace(report.Version) != "" &&
		strings.TrimSpace(report.OS) != "" && strings.TrimSpace(report.Arch) != "" &&
		strings.TrimSpace(report.Message) != ""
}

func sanitizeReport(report Report) Report {
	report.Kind = "crash"
	report.Version = sanitizeField(defaultString(report.Version, "unknown"), 64)
	report.OS = sanitizeField(report.OS, 32)
	report.Arch = sanitizeField(report.Arch, 32)
	report.Message = sanitizeText(report.Message, maxMessageBytes)
	report.SchemaVersion = currentSchemaVersion
	report.Source = "cli.go"
	report.Label = "panic"
	report.ErrorType = sanitizeField(report.ErrorType, 128)
	report.ErrorMessage = sanitizeText(report.ErrorMessage, maxFieldBytes)
	report.Stack = sanitizeStack(report.Stack)
	report.TopFrame = topFrame(report.Stack)
	report.OccurredAt = sanitizeField(report.OccurredAt, 64)
	return report
}

func sanitizeStack(stack string) string {
	stack = sanitizeText(stack, maxStackBytes*2)
	lines := strings.Split(stack, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if location := goLocationPattern.FindString(trimmed); location != "" {
			normalized := strings.ReplaceAll(location, `\`, "/")
			fileAndLine := normalized[strings.LastIndex(normalized, "/")+1:]
			lines[i] = "\t<path>/" + fileAndLine
			continue
		}
		if open := strings.Index(trimmed, "("); open > 0 {
			lines[i] = strings.TrimSpace(trimmed[:open]) + "(...)"
		}
	}
	return clip(strings.TrimSpace(strings.Join(lines, "\n")), maxStackBytes)
}

func topFrame(stack string) string {
	fallback := ""
	functionName := ""
	for line := range strings.SplitSeq(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<path>/") && strings.Contains(line, ".go:") {
			frame := line
			if functionName != "" {
				frame = functionName + " " + line
			}
			if fallback == "" {
				fallback = frame
			}
			if !isCrashCaptureFrame(functionName) {
				return clip(frame, 300)
			}
			functionName = ""
			continue
		}
		if before, ok := strings.CutSuffix(line, "(...)"); ok {
			functionName = strings.TrimSpace(before)
		}
	}
	return clip(fallback, 300)
}

func isCrashCaptureFrame(functionName string) bool {
	return functionName == "runtime/debug.Stack" || functionName == "panic" ||
		strings.HasPrefix(functionName, "runtime.") ||
		strings.Contains(functionName, ".runWithCrashCapture.func")
}

func sanitizeField(value string, max int) string {
	return sanitizeText(value, max)
}

func sanitizeText(value string, max int) string {
	value = userPathSegment.ReplaceAllString(value, "${1}_")
	value = emailPattern.ReplaceAllString(value, "[redacted-email]")
	value = bearerTokenPattern.ReplaceAllString(value, "Bearer [redacted]")
	value = secretKeyValuePattern.ReplaceAllString(value, "${1}=[redacted]")
	value = envIdentifierPattern.ReplaceAllString(value, "[redacted-env]")
	value = jwtPattern.ReplaceAllString(value, "[redacted-jwt]")
	value = explicitKeyPattern.ReplaceAllString(value, "[redacted-key]")
	value = longHexPattern.ReplaceAllString(value, "[redacted-hex]")
	value = longBase64Pattern.ReplaceAllString(value, "[redacted-token]")
	value = longBase64URLPattern.ReplaceAllString(value, "[redacted-token]")
	return clip(strings.TrimSpace(value), max)
}

func clip(value string, max int) string {
	if len(value) <= max {
		return value
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseIDTime(id string) time.Time {
	part, _, _ := strings.Cut(id, "-")
	ns, _ := strconv.ParseInt(part, 10, 64)
	return time.Unix(0, ns).UTC()
}

// CapturedAt returns the local report timestamp encoded in its ID.
func (p Pending) CapturedAt() time.Time { return parseIDTime(p.ID) }
