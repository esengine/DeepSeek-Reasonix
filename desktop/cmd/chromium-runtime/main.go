package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	defaultManifestPath = "build/chromium/manifest.json"
	markerFileName      = ".reasonix-chromium.json"
	toolUserAgent       = "Reasonix-Chromium-Builder/1"
)

var supportedPlatforms = []string{
	"windows-amd64",
	"windows-arm64",
	"linux-amd64",
	"linux-arm64",
	"darwin-amd64",
	"darwin-arm64",
}

type runtimeManifest struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	RuntimeVersion string                     `json:"runtimeVersion"`
	Platforms      map[string]runtimeArtifact `json:"platforms"`
}

type runtimeArtifact struct {
	Source          string   `json:"source"`
	SourceVersion   string   `json:"sourceVersion"`
	SourceRevision  string   `json:"sourceRevision"`
	URL             string   `json:"url"`
	SHA256          string   `json:"sha256"`
	ArchiveSize     int64    `json:"archiveSize"`
	ArchiveRoot     string   `json:"archiveRoot"`
	BundleSource    string   `json:"bundleSource,omitempty"`
	BundleTarget    string   `json:"bundleTarget,omitempty"`
	Executable      string   `json:"executable"`
	ExecutablePaths []string `json:"executablePaths,omitempty"`
	RequiredPaths   []string `json:"requiredPaths"`
}

type preparedMarker struct {
	Platform       string `json:"platform"`
	RuntimeVersion string `json:"runtimeVersion"`
	Source         string `json:"source"`
	SourceVersion  string `json:"sourceVersion"`
	SourceRevision string `json:"sourceRevision"`
	ArchiveSHA256  string `json:"archiveSHA256"`
}

type commandOptions struct {
	manifestPath string
	platform     string
	output       string
	cache        string
	stdout       io.Writer
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "chromium-runtime:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: chromium-runtime <prepare|verify> --platform <goos/goarch>")
	}
	command := args[0]
	if command != "prepare" && command != "verify" {
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := commandOptions{stdout: stdout}
	fs.StringVar(&opts.manifestPath, "manifest", defaultManifestPath, "Chromium manifest path")
	fs.StringVar(&opts.platform, "platform", "", "target platform as goos/goarch or goos-goarch")
	fs.StringVar(&opts.output, "output", "", "prepared runtime output directory")
	fs.StringVar(&opts.cache, "cache", "", "download cache directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	key, goos, goarch, err := normalizePlatform(opts.platform)
	if err != nil {
		return err
	}
	manifest, err := loadManifest(opts.manifestPath)
	if err != nil {
		return err
	}
	if err := validateManifest(manifest); err != nil {
		return err
	}
	artifact, ok := manifest.Platforms[key]
	if !ok {
		return fmt.Errorf("manifest has no Chromium runtime for %s", key)
	}
	if opts.output == "" {
		opts.output = filepath.Join("build", "runtime", "chromium", key)
	}
	if command == "verify" {
		if err := verifyPreparedRuntime(opts.output, key, goos, goarch, manifest.RuntimeVersion, artifact, true); err != nil {
			return missingRuntimeError(key, err)
		}
		fmt.Fprintf(stdout, "Bundled Chromium runtime verified for %s at %s\n", key, opts.output)
		return nil
	}
	if err := verifyPreparedRuntime(opts.output, key, goos, goarch, manifest.RuntimeVersion, artifact, true); err == nil {
		fmt.Fprintf(stdout, "Bundled Chromium runtime already prepared for %s at %s\n", key, opts.output)
		return nil
	}

	if opts.cache == "" {
		opts.cache, err = chromiumCacheDir()
		if err != nil {
			return err
		}
	}
	archivePath, err := ensureArchive(context.Background(), opts.cache, key, artifact)
	if err != nil {
		return err
	}
	if err := prepareRuntime(archivePath, opts.output, key, goos, goarch, manifest.RuntimeVersion, artifact); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Bundled Chromium runtime prepared for %s at %s\n", key, opts.output)
	return nil
}

func loadManifest(name string) (runtimeManifest, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return runtimeManifest{}, fmt.Errorf("read Chromium manifest: %w", err)
	}
	var manifest runtimeManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return runtimeManifest{}, fmt.Errorf("decode Chromium manifest: %w", err)
	}
	return manifest, nil
}

func validateManifest(manifest runtimeManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported Chromium manifest schema %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.RuntimeVersion) == "" {
		return errors.New("Chromium manifest runtimeVersion is required")
	}
	for _, key := range supportedPlatforms {
		artifact, ok := manifest.Platforms[key]
		if !ok {
			return fmt.Errorf("Chromium manifest is missing %s", key)
		}
		if err := validateArtifact(key, artifact); err != nil {
			return err
		}
	}
	for key := range manifest.Platforms {
		if !slices.Contains(supportedPlatforms, key) {
			return fmt.Errorf("Chromium manifest contains unsupported platform %s", key)
		}
	}
	return nil
}

func validateArtifact(key string, artifact runtimeArtifact) error {
	if artifact.Source == "" || artifact.SourceVersion == "" || artifact.SourceRevision == "" {
		return fmt.Errorf("Chromium manifest %s is missing source metadata", key)
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("Chromium manifest %s has a non-fixed HTTPS URL", key)
	}
	if strings.Contains(strings.ToLower(artifact.URL), "latest") {
		return fmt.Errorf("Chromium manifest %s must not use a floating URL", key)
	}
	digest, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("Chromium manifest %s has an invalid SHA256", key)
	}
	if artifact.ArchiveSize <= 0 || artifact.ArchiveRoot == "" || artifact.Executable == "" || len(artifact.RequiredPaths) == 0 {
		return fmt.Errorf("Chromium manifest %s is incomplete", key)
	}
	paths := append(append([]string{}, artifact.RequiredPaths...), artifact.ExecutablePaths...)
	paths = append(paths, artifact.ArchiveRoot, artifact.Executable)
	if artifact.BundleSource != "" || artifact.BundleTarget != "" {
		if artifact.BundleSource == "" || artifact.BundleTarget == "" {
			return fmt.Errorf("Chromium manifest %s bundle rename is incomplete", key)
		}
		paths = append(paths, artifact.BundleSource, artifact.BundleTarget)
	}
	for _, item := range paths {
		if _, err := safeRelativePath(item); err != nil {
			return fmt.Errorf("Chromium manifest %s path %q: %w", key, item, err)
		}
	}
	return nil
}

func normalizePlatform(value string) (key, goos, goarch string, err error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", "", "", errors.New("--platform is required")
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("invalid platform %q", value)
		}
		goos, goarch = parts[0], parts[1]
	} else {
		for _, candidateOS := range []string{"windows", "linux", "darwin"} {
			prefix := candidateOS + "-"
			if strings.HasPrefix(value, prefix) {
				goos, goarch = candidateOS, strings.TrimPrefix(value, prefix)
				break
			}
		}
	}
	key = goos + "-" + goarch
	if !slices.Contains(supportedPlatforms, key) {
		return "", "", "", fmt.Errorf("unsupported Chromium platform %q", value)
	}
	return key, goos, goarch, nil
}

func chromiumCacheDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("REASONIX_CHROMIUM_CACHE")); configured != "" {
		return filepath.Abs(configured)
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve Chromium cache: %w", err)
	}
	return filepath.Join(root, "reasonix", "chromium"), nil
}

func ensureArchive(ctx context.Context, cacheDir, key string, artifact runtimeArtifact) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create Chromium cache: %w", err)
	}
	archivePath := filepath.Join(cacheDir, artifact.SHA256+".zip")
	if err := verifyArchiveFile(archivePath, artifact); err == nil {
		return archivePath, nil
	}
	_ = os.Remove(archivePath)
	tmp, err := os.CreateTemp(cacheDir, ".download-*.zip")
	if err != nil {
		return "", fmt.Errorf("create Chromium download: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		tmp.Close()
		return "", err
	}
	req.Header.Set("User-Agent", toolUserAgent)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("download Chromium for %s: %w", key, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		tmp.Close()
		return "", fmt.Errorf("download Chromium for %s: HTTP %s", key, response.Status)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), response.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download Chromium for %s: %w", key, copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written != artifact.ArchiveSize {
		return "", fmt.Errorf("Chromium archive size mismatch for %s: got %d want %d", key, written, artifact.ArchiveSize)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, artifact.SHA256) {
		return "", fmt.Errorf("Chromium archive SHA256 mismatch for %s: got %s want %s", key, got, artifact.SHA256)
	}
	if err := os.Rename(tmpName, archivePath); err != nil {
		if verifyErr := verifyArchiveFile(archivePath, artifact); verifyErr == nil {
			return archivePath, nil
		}
		return "", fmt.Errorf("cache Chromium archive: %w", err)
	}
	return archivePath, nil
}

func verifyArchiveFile(name string, artifact runtimeArtifact) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	if info.Size() != artifact.ArchiveSize {
		return errors.New("archive size mismatch")
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, artifact.SHA256) {
		return errors.New("archive SHA256 mismatch")
	}
	return nil
}

func prepareRuntime(archivePath, output, key, goos, goarch, runtimeVersion string, artifact runtimeArtifact) error {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(output)+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := extractArchive(archivePath, staging, artifact.ArchiveRoot); err != nil {
		return err
	}
	if artifact.BundleSource != "" {
		from := filepath.Join(staging, filepath.FromSlash(artifact.BundleSource))
		to := filepath.Join(staging, filepath.FromSlash(artifact.BundleTarget))
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("normalize Chromium app bundle: %w", err)
		}
	}
	for _, relative := range artifact.ExecutablePaths {
		name := filepath.Join(staging, filepath.FromSlash(relative))
		if err := os.Chmod(name, 0o755); err != nil {
			return fmt.Errorf("mark Chromium executable %s: %w", relative, err)
		}
	}
	if goos != "windows" && len(artifact.ExecutablePaths) == 0 {
		if err := os.Chmod(filepath.Join(staging, filepath.FromSlash(artifact.Executable)), 0o755); err != nil {
			return err
		}
	}
	marker := preparedMarker{
		Platform:       key,
		RuntimeVersion: runtimeVersion,
		Source:         artifact.Source,
		SourceVersion:  artifact.SourceVersion,
		SourceRevision: artifact.SourceRevision,
		ArchiveSHA256:  artifact.SHA256,
	}
	markerData, _ := json.MarshalIndent(marker, "", "  ")
	markerData = append(markerData, '\n')
	if err := os.WriteFile(filepath.Join(staging, markerFileName), markerData, 0o644); err != nil {
		return err
	}
	if err := verifyPreparedRuntime(staging, key, goos, goarch, runtimeVersion, artifact, true); err != nil {
		return err
	}
	return replaceDirectory(staging, output)
}

func extractArchive(archivePath, output, archiveRoot string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open Chromium archive: %w", err)
	}
	defer reader.Close()
	prefix := strings.TrimSuffix(path.Clean(strings.ReplaceAll(archiveRoot, "\\", "/")), "/") + "/"
	found := false
	for _, entry := range reader.File {
		entryName := strings.TrimPrefix(strings.ReplaceAll(entry.Name, "\\", "/"), "./")
		if entryName == strings.TrimSuffix(prefix, "/") || entryName == prefix {
			continue
		}
		if !strings.HasPrefix(entryName, prefix) {
			return fmt.Errorf("Chromium archive entry %q is outside archiveRoot %q", entry.Name, archiveRoot)
		}
		relative, err := safeRelativePath(strings.TrimPrefix(entryName, prefix))
		if err != nil {
			return fmt.Errorf("unsafe Chromium archive entry %q: %w", entry.Name, err)
		}
		found = true
		destination := filepath.Join(output, filepath.FromSlash(relative))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, directoryMode(entry.Mode())); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			target, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			if err := validateSymlinkTarget(output, filepath.Dir(destination), string(target)); err != nil {
				return fmt.Errorf("unsafe Chromium symlink %q: %w", entry.Name, err)
			}
			if err := os.Symlink(string(target), destination); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported Chromium archive entry type %q", entry.Name)
		}
		if err := writeZipEntry(entry, destination); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("Chromium archive root %q is empty", archiveRoot)
	}
	return nil
}

func safeRelativePath(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	clean := path.Clean(value)
	volumePath := len(clean) >= 2 && clean[1] == ':'
	if clean == "." || clean == "" || path.IsAbs(clean) || volumePath || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes the runtime root")
	}
	return clean, nil
}

func validateSymlinkTarget(root, parent, target string) error {
	target = strings.TrimSpace(target)
	if target == "" || filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
		return errors.New("symlink target must be relative")
	}
	resolved := filepath.Clean(filepath.Join(parent, filepath.FromSlash(target)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("symlink target escapes the runtime root")
	}
	return nil
}

func directoryMode(mode os.FileMode) os.FileMode {
	if mode.Perm() == 0 {
		return 0o755
	}
	return mode.Perm()
}

func readZipEntry(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 1<<20))
}

func writeZipEntry(entry *zip.File, destination string) error {
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	mode := entry.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func replaceDirectory(staging, output string) error {
	backup := output + ".previous"
	_ = os.RemoveAll(backup)
	hadOutput := false
	if _, err := os.Stat(output); err == nil {
		hadOutput = true
		if err := os.Rename(output, backup); err != nil {
			return fmt.Errorf("replace existing Chromium runtime: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		if hadOutput {
			_ = os.Rename(backup, output)
		}
		return fmt.Errorf("install prepared Chromium runtime: %w", err)
	}
	_ = os.RemoveAll(backup)
	return nil
}

func verifyPreparedRuntime(root, key, goos, goarch, runtimeVersion string, artifact runtimeArtifact, checkMarker bool) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("runtime directory does not exist")
	}
	if checkMarker {
		data, err := os.ReadFile(filepath.Join(root, markerFileName))
		if err != nil {
			return errors.New("prepared runtime marker is missing")
		}
		var marker preparedMarker
		if json.Unmarshal(data, &marker) != nil || marker.Platform != key || marker.RuntimeVersion != runtimeVersion || marker.Source != artifact.Source || marker.SourceVersion != artifact.SourceVersion || marker.ArchiveSHA256 != artifact.SHA256 || marker.SourceRevision != artifact.SourceRevision {
			return errors.New("prepared runtime marker does not match the manifest")
		}
	}
	for _, relative := range artifact.RequiredPaths {
		name := filepath.Join(root, filepath.FromSlash(relative))
		if _, err := os.Stat(name); err != nil {
			return fmt.Errorf("required Chromium resource %q is missing", relative)
		}
	}
	executable := filepath.Join(root, filepath.FromSlash(artifact.Executable))
	executableInfo, err := os.Stat(executable)
	if err != nil || executableInfo.IsDir() {
		return fmt.Errorf("Chromium executable %q is missing", artifact.Executable)
	}
	if goos != "windows" && executableInfo.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("Chromium executable %q is not executable", artifact.Executable)
	}
	if goos != "windows" {
		for _, relative := range artifact.ExecutablePaths {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
			if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("Chromium executable resource %q is missing or not executable", relative)
			}
		}
	}
	if err := verifyExecutableArchitecture(executable, goos, goarch); err != nil {
		return err
	}
	return nil
}

func verifyExecutableArchitecture(name, goos, goarch string) error {
	switch goos {
	case "windows":
		file, err := pe.Open(name)
		if err != nil {
			return fmt.Errorf("read Chromium PE architecture: %w", err)
		}
		defer file.Close()
		want := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if goarch == "arm64" {
			want = pe.IMAGE_FILE_MACHINE_ARM64
		}
		if file.FileHeader.Machine != want {
			return fmt.Errorf("Chromium architecture mismatch: PE machine %#x, want %s", file.FileHeader.Machine, goarch)
		}
	case "linux":
		file, err := elf.Open(name)
		if err != nil {
			return fmt.Errorf("read Chromium ELF architecture: %w", err)
		}
		defer file.Close()
		want := elf.EM_X86_64
		if goarch == "arm64" {
			want = elf.EM_AARCH64
		}
		if file.Machine != want {
			return fmt.Errorf("Chromium architecture mismatch: ELF machine %s, want %s", file.Machine, goarch)
		}
	case "darwin":
		want := macho.CpuAmd64
		if goarch == "arm64" {
			want = macho.CpuArm64
		}
		fat, fatErr := macho.OpenFat(name)
		if fatErr == nil {
			defer fat.Close()
			for _, arch := range fat.Arches {
				if arch.Cpu == want {
					return nil
				}
			}
			return fmt.Errorf("Chromium architecture mismatch: Mach-O does not contain %s", goarch)
		}
		file, err := macho.Open(name)
		if err != nil {
			return fmt.Errorf("read Chromium Mach-O architecture: %w", err)
		}
		defer file.Close()
		if file.Cpu != want {
			return fmt.Errorf("Chromium architecture mismatch: Mach-O CPU %s, want %s", file.Cpu, goarch)
		}
	default:
		return fmt.Errorf("unsupported Chromium operating system %s", goos)
	}
	return nil
}

func missingRuntimeError(key string, cause error) error {
	return fmt.Errorf("Bundled Chromium runtime is missing for %s. Run the Chromium preparation step before packaging: %w", key, cause)
}
