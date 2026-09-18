package control

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/attachment"
	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

const maxImageAttachmentBytes = 64 * 1024 * 1024
const maxFileAttachmentBytes = 25 * 1024 * 1024
const maxAttachmentCreateAttempts = 1000

// ErrNoClipboardImage reports that the clipboard was read successfully but holds
// no supported image. It is distinct from a missing clipboard tool: callers
// offering an image-first paste shortcut use it to fall back to text before
// surfacing an image-specific diagnostic.
var ErrNoClipboardImage = errors.New("clipboard does not contain an image")

// ErrUnsupportedClipboardImage marks the more specific no-pasteable-image case
// where the clipboard advertised only image formats Reasonix cannot save.
var ErrUnsupportedClipboardImage = errors.New("clipboard image type is not supported")

type unsupportedClipboardImageError struct {
	tool  string
	types []string
}

func (e unsupportedClipboardImageError) Error() string {
	return fmt.Sprintf("%s offers unsupported image types: %s", e.tool, strings.Join(e.types, ", "))
}

// Unsupported image formats still mean there is no image Reasonix can paste.
// Wrapping the sentinel lets image-first shortcuts try their normal text
// fallback before surfacing the more specific diagnostic.
func (e unsupportedClipboardImageError) Unwrap() []error {
	return []error{ErrNoClipboardImage, ErrUnsupportedClipboardImage}
}

var (
	lookClipboardTool = exec.LookPath
	runClipboardTool  = func(path string, args ...string) ([]byte, []byte, error) {
		cmd := proc.Command(path, args...)
		cmd.Env = secrets.ProcessEnv()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		return out, stderr.Bytes(), err
	}
)

var attachmentPathSeq atomic.Uint64
var attachmentNow = time.Now
var safeAttachmentExt = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)

// SaveAttachmentDataURL stores a non-image file (dropped/pasted in the desktop
// app, where the browser exposes bytes but not a real path) under
// .reasonix/attachments and returns its repo-relative path for @referencing.
// origName supplies only the extension; the stored name is generated.
func SaveAttachmentDataURL(origName, dataURL string) (string, error) {
	return SaveAttachmentDataURLInRoot(".", origName, dataURL)
}

func SaveAttachmentDataURLInRoot(root, origName, dataURL string) (string, error) {
	const marker = ";base64,"
	_, after, ok := strings.Cut(dataURL, marker)
	if !strings.HasPrefix(dataURL, "data:") || !ok {
		return "", fmt.Errorf("unsupported pasted file")
	}
	raw, err := base64.StdEncoding.DecodeString(after)
	if err != nil {
		return "", fmt.Errorf("decode pasted file: %w", err)
	}
	return SaveAttachmentBytesInRoot(root, origName, raw)
}

func SaveAttachmentBytes(origName string, raw []byte) (string, error) {
	return SaveAttachmentBytesInRoot(".", origName, raw)
}

func SaveAttachmentBytesInRoot(root, origName string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func SaveImageDataURL(dataURL string) (string, error) {
	return SaveImageDataURLInRoot(".", dataURL)
}

func SaveImageDataURLInRoot(root, dataURL string) (string, error) {
	const prefix = "data:"
	const marker = ";base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", fmt.Errorf("unsupported pasted image")
	}
	i := strings.Index(dataURL, marker)
	if i <= len(prefix) {
		return "", fmt.Errorf("unsupported pasted image")
	}
	mime := strings.ToLower(dataURL[len(prefix):i])
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil {
		return "", fmt.Errorf("decode pasted image: %w", err)
	}
	return SaveImageBytesInRoot(root, mime, raw)
}

func SaveImageBytes(declaredMime string, raw []byte) (string, error) {
	return SaveImageBytesInRoot(".", declaredMime, raw)
}

func SaveImageBytesInRoot(root, declaredMime string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 64 MB")
	}
	mime := detectedImageMime(raw)
	if mime == "" {
		return "", fmt.Errorf("pasted data is not a supported image")
	}
	if declaredMime != "" && imageExt(declaredMime) == "" {
		return "", fmt.Errorf("unsupported image type: %s", declaredMime)
	}
	ext := imageExt(mime)
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func saveAttachmentBytesInRoot(root, ext string, raw []byte) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ensureAttachmentRootIn(absRoot); err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileIn(absRoot, ext)
	if err != nil {
		return "", err
	}
	if n, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", err
	} else if n != len(raw) {
		_ = f.Close()
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", io.ErrShortWrite
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func SaveImageFile(path string) (string, error) {
	return SaveImageFileInRoot(".", path)
}

func SaveImageFileInRoot(root, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("pasted image path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 64 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("pasted image changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 64 MB")
	}
	if after, err := f.Stat(); err != nil {
		return "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return "", fmt.Errorf("pasted image changed while reading")
	}
	return SaveImageBytesInRoot(root, "", raw)
}

func SaveAttachmentFile(path string) (string, error) {
	return SaveAttachmentFileInRoot(".", path)
}

func SaveAttachmentFileInRoot(root, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("attachment path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("attachment changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxFileAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	if after, err := f.Stat(); err != nil {
		return "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return "", fmt.Errorf("attachment changed while reading")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func SaveClipboardImage() (string, error) {
	return SaveClipboardImageInRoot(".")
}

func SaveClipboardImageInRoot(root string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return saveDarwinClipboardImageInRoot(root)
	case "windows":
		return saveWindowsClipboardImageInRoot(root)
	case "linux":
		return saveLinuxClipboardImageInRoot(root)
	default:
		return "", fmt.Errorf("clipboard image paste is not supported on %s yet", runtime.GOOS)
	}
}

func saveWindowsClipboardImageInRoot(root string) (string, error) {
	// Windows PowerShell 5.1 (preinstalled) reaches the GUI clipboard; pwsh (Core)
	// lacks Get-Clipboard -Format Image, so invoke powershell.exe. The PNG is
	// returned as base64 on stdout so no temp file is involved.
	script := `Add-Type -AssemblyName System.Drawing
$img = Get-Clipboard -Format Image
if ($null -eq $img) { [Console]::Error.WriteLine('clipboard has no image'); exit 1 }
$ms = New-Object System.IO.MemoryStream
$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
[Convert]::ToBase64String($ms.ToArray())`
	cmd := proc.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("read clipboard image: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("read clipboard image: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("decode clipboard image: %w", err)
	}
	return SaveImageBytesInRoot(root, "", raw)
}

// clipboardImageTypes lists the image mimes we can save, most preferred
// first; Wayland compositors and screenshot apps offer any of these.
var clipboardImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

func clipboardImageReadArgs(tool, mime string) []string {
	if tool == "wl-paste" {
		return []string{"--type", mime, "--no-newline"}
	}
	return []string{"-selection", "clipboard", "-t", mime, "-o"}
}

func saveLinuxClipboardImage() (string, error) {
	return saveLinuxClipboardImageInRoot(".")
}

func saveLinuxClipboardImageInRoot(root string) (string, error) {
	type clipboardTool struct {
		name      string
		typesArgs []string
	}
	tools := []clipboardTool{
		{name: "wl-paste", typesArgs: []string{"--list-types"}},
		{name: "xclip", typesArgs: []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}},
	}
	foundTool := false
	confirmedNoImage := false
	var probeFailures, readFailures []error
	for _, tool := range tools {
		path, err := lookClipboardTool(tool.name)
		if err != nil {
			continue
		}
		foundTool = true
		types, stderr, err := runClipboardTool(path, tool.typesArgs...)
		if err != nil {
			if clipboardProbeMeansNoImage(tool.name, stderr) {
				confirmedNoImage = true
				continue
			}
			probeFailures = append(probeFailures, fmt.Errorf("probe %s clipboard types: %w", tool.name, err))
			continue
		}
		mime := ""
		for _, want := range clipboardImageTypes {
			if clipboardTypeListed(types, want) {
				mime = want
				break
			}
		}
		if mime == "" {
			if offered := offeredImageTypes(types); len(offered) > 0 {
				readFailures = append(readFailures, unsupportedClipboardImageError{tool: tool.name, types: offered})
				continue
			}
			confirmedNoImage = true
			continue
		}
		out, _, err := runClipboardTool(path, clipboardImageReadArgs(tool.name, mime)...)
		if err != nil {
			readFailures = append(readFailures, fmt.Errorf("read clipboard image with %s: %w", tool.name, err))
			continue
		}
		if len(out) == 0 {
			readFailures = append(readFailures, fmt.Errorf("read clipboard image with %s: empty image data", tool.name))
			continue
		}
		rel, err := SaveImageBytesInRoot(root, "", out)
		if err != nil {
			readFailures = append(readFailures, fmt.Errorf("save clipboard image from %s: %w", tool.name, err))
			continue
		}
		return rel, nil
	}
	if !foundTool {
		return "", fmt.Errorf("clipboard image paste needs wl-paste (Wayland) or xclip (X11)")
	}
	if len(readFailures) > 0 {
		return "", fmt.Errorf("read clipboard image: %w", errors.Join(readFailures...))
	}
	if confirmedNoImage {
		return "", ErrNoClipboardImage
	}
	return "", fmt.Errorf("read clipboard image: %w", errors.Join(probeFailures...))
}

func clipboardTypeListed(raw []byte, want string) bool {
	for field := range strings.FieldsSeq(string(raw)) {
		if strings.EqualFold(field, want) {
			return true
		}
	}
	return false
}

// offeredImageTypes returns safely quoted image/* MIME names that Reasonix
// cannot save. Clipboard owners control these strings, so errors must never
// contain their terminal control sequences verbatim.
func offeredImageTypes(raw []byte) []string {
	var offered []string
	for field := range strings.FieldsSeq(string(raw)) {
		lower := strings.ToLower(field)
		if strings.HasPrefix(lower, "image/") && !slices.Contains(clipboardImageTypes, lower) {
			offered = append(offered, strconv.QuoteToASCII(field))
		}
	}
	return offered
}

func clipboardProbeMeansNoImage(tool string, stderr []byte) bool {
	message := string(stderr)
	switch tool {
	case "wl-paste":
		return strings.Contains(message, "Nothing is copied")
	case "xclip":
		return strings.Contains(message, "There is no owner for the") && strings.Contains(message, "selection")
	default:
		return false
	}
}

func ImageDataURL(path string) (string, error) {
	return ImageDataURLInRoot(".", path)
}

func ImageDataURLInRoot(root, path string) (string, error) {
	raw, mime, err := readAttachmentImageInRoot(root, path)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// visionImageDataURLInRoot prepares a bounded request image from a workspace attachment.
func visionImageDataURLInRoot(root, path string) (string, error) {
	raw, mime, err := readAttachmentImageInRoot(root, path)
	if err != nil {
		return "", err
	}
	raw, mime = compressForVision(raw, mime)
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func readAttachmentImageInRoot(root, path string) (raw []byte, mime string, err error) {
	if _, err := cleanAttachmentPathInRoot(root, path); err != nil {
		return nil, "", err
	}
	raw, err = attachment.ReadWorkspaceImageBytes(root, path, maxImageAttachmentBytes)
	if err != nil {
		return nil, "", err
	}
	mime = detectedImageMime(raw)
	if mime == "" {
		return nil, "", fmt.Errorf("attachment is not an image")
	}
	return raw, mime, nil
}

// ValidateAttachmentInRoot checks a persisted attachment without loading its
// contents. The bounded root prevents parent replacement from escaping the
// workspace and Lstat rejects a final symlink.
func ValidateAttachmentInRoot(root, path string) error {
	clean, err := cleanAttachmentPathInRoot(root, path)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, clean)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("attachment path is outside workspace")
	}
	bounded, err := os.OpenRoot(absRoot)
	if err != nil {
		return err
	}
	defer bounded.Close()
	info, err := bounded.Lstat(rel)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("attachment path must not be a symlink")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("attachment is missing or empty")
	}
	f, err := bounded.OpenFile(rel, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("attachment changed while opening")
	}
	return nil
}

func cleanAttachmentPathInRoot(base, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("attachment path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	relRoot := filepath.Join(".reasonix", "attachments")
	if clean == "." || clean == relRoot || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !strings.HasPrefix(clean, relRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("attachment path is outside .reasonix/attachments")
	}
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	root := filepath.Join(absBase, relRoot)
	if err := validateAttachmentDirectories(absBase); err != nil {
		return "", err
	}
	absPath := filepath.Join(absBase, clean)
	if err := rejectSymlinkComponents(absPath, root); err != nil {
		return "", err
	}
	return absPath, nil
}

func rejectSymlinkComponents(path, root string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("attachment path is outside .reasonix/attachments")
	}
	cur := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment path must not contain symlinks")
		}
	}
	return nil
}

func ensureAttachmentRoot() error {
	return ensureAttachmentRootIn(".")
}

func ensureAttachmentRootIn(base string) error {
	if info, err := os.Lstat(filepath.Join(base, ".reasonix")); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment path must not contain symlinks")
		}
		if !info.IsDir() {
			return fmt.Errorf("attachment path exists but is not a directory")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	root := filepath.Join(base, ".reasonix", "attachments")
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment directory must not be a symlink")
		}
		if !info.IsDir() {
			return fmt.Errorf("attachment path exists but is not a directory")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("attachment directory is invalid")
	}
	return nil
}

func validateAttachmentDirectories(base string) error {
	for _, path := range []string{
		filepath.Join(base, ".reasonix"),
		filepath.Join(base, ".reasonix", "attachments"),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment path must not contain symlinks")
		}
		if !info.IsDir() {
			return fmt.Errorf("attachment path exists but is not a directory")
		}
	}
	return nil
}

func saveDarwinClipboardImageInRoot(root string) (string, error) {
	return saveDarwinClipboardImageWith(func(class string) (string, error) {
		return saveDarwinClipboardClassInRoot(root, class)
	})
}

func saveDarwinClipboardImageWith(readClass func(string) (string, error)) (string, error) {
	for _, class := range []string{"PNGf", "JPEG"} {
		rel, err := readClass(class)
		if err == nil {
			return rel, nil
		}
		if !errors.Is(err, ErrNoClipboardImage) {
			return "", err
		}
	}
	return "", ErrNoClipboardImage
}

func saveDarwinClipboardClassInRoot(root, class string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ensureAttachmentRootIn(absRoot); err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileIn(absRoot, ".bin")
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", err
	}
	abs := filepath.Join(absRoot, rel)
	const noImageMarker = "__REASONIX_NO_CLIPBOARD_IMAGE__"
	script := fmt.Sprintf(`
set hasImageType to false
repeat with typeEntry in (clipboard info)
	if (item 1 of typeEntry) is «class %s» then
		set hasImageType to true
		exit repeat
	end if
end repeat
if not hasImageType then return %q
set outPath to POSIX file %q
set img to the clipboard as «class %s»
set f to open for access outPath with write permission
try
	set eof f to 0
	write img to f
	close access f
on error errMsg
	try
		close access f
	end try
	error errMsg
end try
	`, class, noImageMarker, abs, class)
	clip := proc.Command("osascript", "-e", script)
	clip.Env = secrets.ProcessEnv()
	out, runErr := clip.CombinedOutput()
	if err := classifyDarwinClipboardResult(out, runErr, noImageMarker); err != nil {
		_ = os.Remove(abs)
		return "", err
	}
	raw, err := os.ReadFile(abs)
	_ = os.Remove(abs)
	if err != nil {
		return "", err
	}
	return SaveImageBytesInRoot(absRoot, "", raw)
}

func classifyDarwinClipboardResult(out []byte, runErr error, noImageMarker string) error {
	detail := strings.TrimSpace(string(out))
	if runErr == nil {
		if detail == noImageMarker {
			return ErrNoClipboardImage
		}
		return nil
	}
	if detail == "" {
		return fmt.Errorf("read clipboard image: %w", runErr)
	}
	return fmt.Errorf("read clipboard image: %s: %w", detail, runErr)
}

func createAttachmentFile(ext string) (string, *os.File, error) {
	return createAttachmentFileIn(".", ext)
}

func createAttachmentFileIn(base, ext string) (string, *os.File, error) {
	for range maxAttachmentCreateAttempts {
		rel := attachmentPath(ext)
		f, err := os.OpenFile(filepath.Join(base, rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return rel, f, nil
	}
	return "", nil, fmt.Errorf("create unique attachment path")
}

func attachmentPath(ext string) string {
	seq := attachmentPathSeq.Add(1)
	name := fmt.Sprintf("clipboard-%s-%06d%s", attachmentNow().Format("20060102-150405.000000"), seq, ext)
	return filepath.Join(".reasonix", "attachments", name)
}

func detectedImageMime(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	mime := http.DetectContentType(raw[:min(len(raw), 512)])
	if imageExt(mime) == "" {
		return ""
	}
	return mime
}

func imageExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}
