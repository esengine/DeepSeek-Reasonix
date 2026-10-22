package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"reasonix/internal/contract/tool"
)

const artifactRedirectLimit = 5

// fetch follows redirects across origins because signed media is usually
// served from a CDN host; the dial guard re-vets every hop.
func (w artifactDownload) fetch(ctx context.Context, raw string) (*http.Response, []string, error) {
	client := ssrfGuardedClient((webFetch{proxySpec: w.proxySpec}).proxyURLFor)
	client.Timeout = artifactDownloadTimeout
	hosts := []string{}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > artifactRedirectLimit {
			return artifactRedirectError("more than 5 redirects")
		}
		if webImageURL(req.URL.String(), nil) == nil || req.URL.User != nil {
			return artifactRedirectError("redirect target is not a plain HTTP(S) address")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return artifactRedirectError("redirect downgraded HTTPS")
		}
		if prev := via[len(via)-1].URL; prev.Scheme != req.URL.Scheme || prev.Host != req.URL.Host {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
		}
		hosts = append(hosts, req.URL.Host)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "reasonix-artifact/1.0")
	resp, err := client.Do(req)
	if err != nil {
		var refusal tool.Refusal
		if errors.As(err, &refusal) {
			return nil, nil, refusal
		}
		return nil, nil, artifactTransferError{cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.StatusCode == http.StatusPartialContent {
		resp.Body.Close()
		return nil, nil, tool.Refusal{Code: codeArtifactDownloadFailed, Message: fmt.Sprintf("artifact source returned HTTP %d", resp.StatusCode)}
	}
	return resp, hosts, nil
}

const codeArtifactDownloadFailed = "artifact.download_failed"

func artifactRedirectError(why string) error {
	return tool.Refusal{Code: CodeArtifactRedirectRefused, Message: "artifact redirect refused: " + why}
}

// Signed addresses must not enter model-visible transport errors.
type artifactTransferError struct{ cause error }

func (artifactTransferError) Error() string {
	return "artifact request failed (refusal: artifact.download_failed)"
}
func (e artifactTransferError) Unwrap() error     { return e.cause }
func (artifactTransferError) RefusalCode() string { return codeArtifactDownloadFailed }

// publishArtifact links the media before writing its receipt, so a receipt
// always names bytes that exist; neither step ever replaces a file.
func publishArtifact(root *os.Root, stage string, source artifactSourceReceipt) error {
	if err := linkCreateOnly(root, stage, source.Path); err != nil {
		return err
	}
	if err := writeCreateOnly(root, source.ProvenancePath, source); err != nil {
		_ = root.Remove(source.Path)
		return err
	}
	return nil
}

func linkCreateOnly(root *os.Root, stage, target string) error {
	err := root.Link(stage, target)
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return artifactExistsError(target)
	}
	src, openErr := root.Open(stage)
	if openErr != nil {
		return errors.Join(err, openErr)
	}
	defer src.Close()
	dst, createErr := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if createErr != nil {
		if errors.Is(createErr, os.ErrExist) {
			return artifactExistsError(target)
		}
		return createErr
	}
	_, copyErr := io.Copy(dst, src)
	copyErr = errors.Join(copyErr, dst.Sync(), dst.Close())
	if copyErr != nil {
		_ = root.Remove(target)
	}
	return copyErr
}

func writeCreateOnly(root *os.Root, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return artifactExistsError(path)
		}
		return err
	}
	if err := errors.Join(writeAll(f, data), f.Sync(), f.Close()); err != nil {
		_ = root.Remove(path)
		return err
	}
	return nil
}

func writeAll(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}
