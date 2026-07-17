package control

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/proc"

	"mvdan.cc/sh/v3/shell"
)

const imageUnderstandingCommandTimeout = 5 * time.Second
const imageUnderstandingCommandMaxConcurrency = 4
const imageUnderstandingCacheVersion = 1

// CommandImageUnderstanding runs a user-configured local OCR/vision sidecar.
// The command is called once per image with the local image path appended as the
// final argument. It may return a ready-made <image-understanding> block, JSON,
// or plain text.
type CommandImageUnderstanding struct {
	argv    []string
	timeout time.Duration
	cache   string
}

type imageUnderstandingCacheEntry struct {
	Version   int    `json:"version"`
	Key       string `json:"key"`
	SHA256    string `json:"sha256"`
	Command   string `json:"command"`
	Block     string `json:"block"`
	CreatedAt string `json:"created_at"`
}

func NewCommandImageUnderstanding(command string) (*CommandImageUnderstanding, error) {
	fields, err := shell.Fields(strings.TrimSpace(command), os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("parse image_understanding_command: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("image_understanding_command is empty")
	}
	return &CommandImageUnderstanding{argv: fields, timeout: imageUnderstandingCommandTimeout}, nil
}

func NewCommandImageUnderstandingForRoot(command, workspaceRoot string) (*CommandImageUnderstanding, error) {
	iu, err := NewCommandImageUnderstanding(command)
	if err != nil {
		return nil, err
	}
	iu.cache = ImageUnderstandingCachePathForRoot(workspaceRoot)
	return iu, nil
}

func ImageUnderstandingCachePathForRoot(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".reasonix", "image-understanding-cache.jsonl")
}

func (u *CommandImageUnderstanding) DescribeImages(ctx context.Context, userInput string, images []ImageUnderstandingRef) (string, error) {
	if u == nil || len(u.argv) == 0 {
		return "", fmt.Errorf("image understanding command is not initialized")
	}
	filtered := make([]ImageUnderstandingRef, 0, len(images))
	for _, img := range images {
		if strings.TrimSpace(img.Path) == "" {
			continue
		}
		filtered = append(filtered, img)
	}
	if len(filtered) == 0 {
		return "", nil
	}
	results := make([]string, len(filtered))
	signature := u.cacheSignature()
	cache := u.loadCache()
	var misses []int
	for i, img := range filtered {
		if block, ok := cache[imageUnderstandingCacheKey(signature, img.SHA256)]; ok {
			results[i] = rewriteImageUnderstandingBlockSource(block, img)
			continue
		}
		misses = append(misses, i)
	}

	type imageResult struct {
		idx   int
		block string
		err   error
	}
	ch := make(chan imageResult, len(misses))
	sem := make(chan struct{}, imageUnderstandingCommandMaxConcurrency)
	for _, idx := range misses {
		idx := idx
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			block, err := u.describeOne(ctx, userInput, filtered[idx])
			ch <- imageResult{idx: idx, block: strings.TrimSpace(block), err: err}
		}()
	}

	var firstErr error
	var entries []imageUnderstandingCacheEntry
	for range misses {
		res := <-ch
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if res.block == "" {
			continue
		}
		results[res.idx] = res.block
		img := filtered[res.idx]
		if strings.TrimSpace(img.SHA256) != "" {
			entries = append(entries, imageUnderstandingCacheEntry{
				Version:   imageUnderstandingCacheVersion,
				Key:       imageUnderstandingCacheKey(signature, img.SHA256),
				SHA256:    img.SHA256,
				Command:   signature,
				Block:     res.block,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	if firstErr != nil {
		return "", firstErr
	}
	u.appendCache(entries)

	var blocks []string
	for _, block := range results {
		if strings.TrimSpace(block) != "" {
			blocks = append(blocks, strings.TrimSpace(block))
		}
	}
	return strings.Join(blocks, "\n\n"), nil
}

func (u *CommandImageUnderstanding) describeOne(ctx context.Context, userInput string, img ImageUnderstandingRef) (string, error) {
	timeout := u.timeout
	if timeout <= 0 {
		timeout = imageUnderstandingCommandTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append([]string{}, u.argv[1:]...)
	args = append(args, img.Path)
	cmd := exec.CommandContext(runCtx, u.argv[0], args...)
	setShellKillTree(cmd)
	cmd.WaitDelay = time.Second
	proc.HideWindow(cmd)
	cmd.Env = append(os.Environ(),
		"REASONIX_IMAGE_SOURCE="+img.Source,
		"REASONIX_IMAGE_SHA256="+img.SHA256,
		"REASONIX_USER_INPUT="+userInput,
	)
	var stdout limitedBuffer
	var stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	waitErr := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("image understanding command timed out")
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			if stderr.Truncated() {
				msg += "\n[truncated]"
			}
			return "", fmt.Errorf("image understanding command failed: %w: %s", waitErr, msg)
		}
		return "", fmt.Errorf("image understanding command failed: %w", waitErr)
	}
	if stdout.Truncated() {
		return normalizeImageUnderstandingOutput(img, stdout.String()+"\n[truncated]"), nil
	}
	return normalizeImageUnderstandingOutput(img, stdout.String()), nil
}

func (u *CommandImageUnderstanding) loadCache() map[string]string {
	out := map[string]string{}
	path := strings.TrimSpace(u.cache)
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry imageUnderstandingCacheEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Version != imageUnderstandingCacheVersion || strings.TrimSpace(entry.Key) == "" || strings.TrimSpace(entry.Block) == "" {
			continue
		}
		out[entry.Key] = entry.Block
	}
	return out
}

func (u *CommandImageUnderstanding) appendCache(entries []imageUnderstandingCacheEntry) {
	if len(entries) == 0 || strings.TrimSpace(u.cache) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(u.cache), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(u.cache, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, entry := range entries {
		_ = enc.Encode(entry)
	}
}

func (u *CommandImageUnderstanding) cacheSignature() string {
	if u == nil || len(u.argv) == 0 {
		return ""
	}
	parts := []string{strings.Join(u.argv, "\x00")}
	if exe, err := exec.LookPath(u.argv[0]); err == nil {
		parts = append(parts, exe)
		if st, err := os.Stat(exe); err == nil {
			parts = append(parts, strconv.FormatInt(st.ModTime().UnixNano(), 10), strconv.FormatInt(st.Size(), 10))
		}
	}
	return strings.Join(parts, "\x00")
}

func imageUnderstandingCacheKey(commandSignature, sha string) string {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("v%d\x00%s\x00%s", imageUnderstandingCacheVersion, commandSignature, sha)))
	return hex.EncodeToString(sum[:])
}

func rewriteImageUnderstandingBlockSource(block string, img ImageUnderstandingRef) string {
	block = strings.TrimSpace(block)
	if !strings.HasPrefix(block, "<image-understanding") {
		return normalizeImageUnderstandingOutput(img, block)
	}
	newOpen := imageUnderstandingOpenTag(img.Source, img.SHA256)
	if idx := strings.Index(block, "\n"); idx >= 0 {
		return newOpen + block[idx:]
	}
	return newOpen + "\n</image-understanding>"
}

func normalizeImageUnderstandingOutput(img ImageUnderstandingRef, out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if strings.HasPrefix(out, "<image-understanding") {
		return out
	}
	var payload map[string]any
	if json.Unmarshal([]byte(out), &payload) == nil && len(payload) > 0 {
		visible := firstJSONText(payload, "visible_text", "text", "ocr", "ocr_text")
		uiState := firstJSONText(payload, "ui_state", "summary", "description")
		errorsText := firstJSONText(payload, "errors", "error")
		layout := firstJSONText(payload, "layout")
		if layout == "" {
			layout = jsonLayoutSummary(payload)
		}
		confidence := firstJSONText(payload, "confidence")
		if confidence == "" {
			confidence = "medium"
		}
		return formatImageUnderstandingBlock(img.Source, img.SHA256, visible, uiState, errorsText, layout, confidence)
	}
	return formatImageUnderstandingBlock(img.Source, img.SHA256, out, "", "", "", "medium")
}

func firstJSONText(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s := jsonValueText(v); strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func jsonValueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := jsonValueText(item); strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := firstJSONText(x, "text", "value", "label"); text != "" {
			return text
		}
		raw, _ := json.Marshal(x)
		return string(raw)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}

func jsonLayoutSummary(payload map[string]any) string {
	var parts []string
	width, wok := jsonNumber(payload["width"])
	height, hok := jsonNumber(payload["height"])
	if wok && hok {
		parts = append(parts, fmt.Sprintf("%gx%g", width, height))
	}
	switch regions := payload["text_regions"].(type) {
	case []any:
		parts = append(parts, fmt.Sprintf("text_regions=%d", len(regions)))
	case float64:
		parts = append(parts, fmt.Sprintf("text_regions=%d", int(regions)))
	}
	if elapsed, ok := jsonNumber(payload["elapsed_ms"]); ok {
		parts = append(parts, fmt.Sprintf("ocr_ms=%.1f", elapsed))
	}
	return strings.Join(parts, "; ")
}

func jsonNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func imageRefSource(r ref) string {
	source := strings.TrimSpace(r.raw)
	if source == "" {
		source = strings.TrimSpace(r.displayPath)
	}
	if source == "" {
		source = strings.TrimSpace(r.path)
	}
	if source == "" {
		return ""
	}
	if strings.HasPrefix(source, "@") {
		return source
	}
	return "@" + filepath.ToSlash(source)
}
