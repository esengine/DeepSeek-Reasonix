package main

import (
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MarkdownImageView is the renderer-safe form of an assistant-provided image
// source. URL is always either a vetted data URI or a same-origin media/proxy
// URL; native file:// paths are never handed to the WebView as image sources.
type MarkdownImageView struct {
	URL       string `json:"url"`
	Filename  string `json:"filename,omitempty"`
	Mime      string `json:"mime,omitempty"`
	Size      int64  `json:"size,omitempty"`
	OpenHref  string `json:"openHref,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

const markdownDataImageMaxEncodedBytes = remoteMarkdownImageMaxBytes*4/3 + 1024

// ResolveMarkdownImageForTab applies a tag-specific policy that is deliberately
// stricter than ordinary Markdown links.
func (a *App) ResolveMarkdownImageForTab(tabID, source string) MarkdownImageView {
	source = strings.TrimSpace(source)
	if source == "" {
		return MarkdownImageView{ErrorCode: "invalid-source"}
	}
	if strings.HasPrefix(source, "//") {
		source = "https:" + source
	}
	if strings.HasPrefix(strings.ToLower(source), "http://") || strings.HasPrefix(strings.ToLower(source), "https://") {
		remote, err := validateRemoteMarkdownImageURL(source)
		if err != nil {
			return MarkdownImageView{OpenHref: source, ErrorCode: "blocked-remote"}
		}
		name := filepath.Base(strings.TrimSpace(mustURLPath(remote)))
		if name == "." || name == string(filepath.Separator) {
			name = "image"
		}
		return MarkdownImageView{
			URL:      remoteMarkdownImagePath + "?url=" + url.QueryEscape(remote),
			Filename: name,
			OpenHref: remote,
		}
	}
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		return resolveMarkdownDataImage(source)
	}

	path, err := a.authorizedMarkdownImagePath(tabID, source)
	if err != nil {
		code := "invalid-path"
		if errors.Is(err, os.ErrPermission) {
			code = "forbidden"
		} else if errors.Is(err, os.ErrNotExist) {
			code = "not-found"
		}
		return MarkdownImageView{ErrorCode: code}
	}
	info, err := os.Stat(path)
	if err != nil {
		return MarkdownImageView{ErrorCode: "not-found"}
	}
	openHref := localFileHref(path)
	if info.IsDir() || !info.Mode().IsRegular() {
		return MarkdownImageView{OpenHref: openHref, ErrorCode: "not-a-file"}
	}
	kind, mimeType := previewMediaKind(path)
	if kind != "image" {
		return MarkdownImageView{Filename: info.Name(), Size: info.Size(), OpenHref: openHref, ErrorCode: "unsupported-type"}
	}
	token := a.ensureMediaTokenStore().create(path, info.Name(), mimeType, kind, info.Size(), info.ModTime())
	return MarkdownImageView{
		URL:      "/__reasonix_workspace_media/" + token + "/" + url.PathEscape(info.Name()),
		Filename: info.Name(),
		Mime:     mimeType,
		Size:     info.Size(),
		OpenHref: openHref,
	}
}

func mustURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func resolveMarkdownDataImage(source string) MarkdownImageView {
	if len(source) > markdownDataImageMaxEncodedBytes {
		return MarkdownImageView{ErrorCode: "too-large"}
	}
	comma := strings.IndexByte(source, ',')
	if comma <= len("data:") {
		return MarkdownImageView{ErrorCode: "invalid-data"}
	}
	header := strings.ToLower(strings.TrimSpace(source[len("data:"):comma]))
	parts := strings.Split(header, ";")
	declared := strings.TrimSpace(parts[0])
	allowed := declared == "image/png" || declared == "image/jpeg" || declared == "image/gif" || declared == "image/webp"
	if !allowed {
		return MarkdownImageView{ErrorCode: "unsupported-type"}
	}
	base64Encoded := false
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "base64" {
			base64Encoded = true
			continue
		}
		return MarkdownImageView{ErrorCode: "invalid-data"}
	}
	payload := source[comma+1:]
	var data []byte
	var err error
	if base64Encoded {
		data, err = base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == '\t' || r == ' ' {
				return -1
			}
			return r
		}, payload))
	} else {
		var decoded string
		decoded, err = url.PathUnescape(payload)
		data = []byte(decoded)
	}
	if err != nil || len(data) == 0 {
		return MarkdownImageView{ErrorCode: "invalid-data"}
	}
	if len(data) > remoteMarkdownImageMaxBytes {
		return MarkdownImageView{ErrorCode: "too-large"}
	}
	_, detected := safeRemoteMarkdownImage(data)
	if detected != declared || detected == "image/svg+xml" {
		return MarkdownImageView{ErrorCode: "invalid-data"}
	}
	return MarkdownImageView{URL: source, Mime: detected, Size: int64(len(data))}
}

func (a *App) authorizedMarkdownImagePath(tabID, source string) (string, error) {
	root, ctrl, ok := a.workspaceTargetForTab(tabID)
	if !ok {
		return "", os.ErrNotExist
	}
	if browser := externalFolderRefBrowserFromController(ctrl); browser != nil {
		if external, _, found := browser.ExternalFolderRefLocalPath(source); found {
			return filepath.Clean(external), nil
		}
	}

	pathSource, err := markdownFileSourcePath(source)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(pathSource) {
		if authorizer, ok := ctrl.(interface {
			AuthorizedExternalFolderLocalPath(string) (string, bool)
		}); ok {
			if external, allowed := authorizer.AuthorizedExternalFolderLocalPath(pathSource); allowed {
				return external, nil
			}
		}
	}
	base, err := workspaceBaseFromRoot(root)
	if err != nil {
		return "", err
	}
	candidate, inside, err := workspacePathForBase(base, pathSource)
	if err != nil || !inside {
		return "", os.ErrPermission
	}
	return canonicalPathWithin(base, candidate)
}

func markdownFileSourcePath(source string) (string, error) {
	// Raw Windows drive/UNC paths are valid Markdown image sources even though
	// net/url would otherwise interpret the drive letter as a URL scheme.
	if filepath.IsAbs(source) && !strings.ContainsAny(source, "?#") {
		return filepath.Clean(source), nil
	}
	if strings.HasPrefix(strings.ToLower(source), "file:") {
		u, err := url.Parse(source)
		if err != nil || !strings.EqualFold(u.Scheme, "file") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", os.ErrInvalid
		}
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return "", os.ErrPermission
		}
		path, err := url.PathUnescape(u.Path)
		if err != nil {
			return "", os.ErrInvalid
		}
		if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		return filepath.FromSlash(path), nil
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "" {
		return "", os.ErrInvalid
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil || strings.ContainsRune(path, 0) {
		return "", os.ErrInvalid
	}
	return filepath.FromSlash(path), nil
}

func canonicalPathWithin(root, candidate string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}
	return filepath.Clean(realCandidate), nil
}

func localFileHref(path string) string {
	slash := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && len(slash) >= 2 && slash[1] == ':' {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}
