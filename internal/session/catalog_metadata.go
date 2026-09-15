package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

const catalogMetadataVersion = 1

const (
	MetadataReady   = "ready"
	MetadataPending = "pending"
	MetadataFailed  = "failed"
)

type catalogMetadata struct {
	Version       int    `json:"version"`
	Codec         string `json:"codec"`
	SessionID     string `json:"sessionId"`
	CreatedAt     string `json:"createdAt"`
	Sequence      uint64 `json:"sequence"`
	Title         string `json:"title,omitempty"`
	ModelRef      string `json:"modelRef,omitempty"`
	ModelIdentity string `json:"modelIdentity,omitempty"`
	Turns         int    `json:"turns"`
	Preview       string `json:"preview,omitempty"`
	// LogRevision pins the cache to the exact durable bytes it was built from.
	// Catalog listing validates this instead of replaying the log, so a page of
	// long sessions costs one stat per session rather than a full scan.
	LogSize      int64  `json:"logSize"`
	LogModTimeNS int64  `json:"logModTimeNs"`
	LogIdentity  string `json:"logIdentity,omitempty"`
}

func catalogMetadataPath(cacheDir string) string {
	return filepath.Join(cacheDir, "catalog-metadata.json")
}

// firstUserPreview returns the first user-authored text suitable as a list
// preview. Host-injected directives (session-context snapshot, reasoning
// language) ride the first user turn: Origin is the authoritative
// provenance, with text recognition only for legacy records recorded before
// the field existed. Injections are also prepended to real user turns, so
// RawContent - the user's exact text - is preferred over Content.
func firstUserPreview(messages []provider.Message) string {
	for _, message := range messages {
		if message.Role != provider.RoleUser {
			continue
		}
		if message.Origin == provider.MessageOriginHost {
			continue
		}
		text := strings.TrimSpace(message.RawContent)
		if text == "" {
			text = strings.TrimSpace(message.Content)
		}
		text = stripLeadingInjectedBlocks(text)
		if text == "" {
			continue
		}
		return messagePreview(provider.Message{Content: text})
	}
	return ""
}

// previewInjectedTags lists the leading block tags hosts prepend to user
// turns. Keep in sync with agent.TransientUserBlockTags; session cannot
// import agent (agent imports session), so the list is duplicated with this
// pointer back.
var previewInjectedTags = []string{"session-context", "reasoning-language", "response-language", "memory-update", "background-jobs", "active-goal", "hook-context", "capability-route", "interrupted-turn-recovery", "execution-policy"}

// stripLeadingInjectedBlocks removes well-formed leading <tag>...</tag>
// blocks the host injected before the user's own words, so a preview never
// leaks host directives regardless of how they were recorded.
func stripLeadingInjectedBlocks(text string) string {
	for {
		rest, stripped := stripOneLeadingInjectedBlock(text)
		if !stripped {
			return text
		}
		text = rest
	}
}

func stripOneLeadingInjectedBlock(text string) (string, bool) {
	s := strings.TrimLeft(text, " \t\r\n")
	for _, tag := range previewInjectedTags {
		open := "<" + tag + ">"
		openAttr := "<" + tag + " "
		var start int
		if strings.HasPrefix(s, open) {
			start = len(open)
		} else if strings.HasPrefix(s, openAttr) {
			if i := strings.Index(s, ">"); i >= 0 {
				start = i + 1
			} else {
				continue
			}
		} else {
			continue
		}
		close := "</" + tag + ">"
		end := strings.Index(s[start:], close)
		if end < 0 {
			continue
		}
		return strings.TrimLeft(s[start+end+len(close):], " \t\r\n"), true
	}
	return text, false
}

func metadataFromProjection(manifest Manifest, sequence uint64, projection Projection) catalogMetadata {
	metadata := catalogMetadata{
		Version: catalogMetadataVersion, Codec: Codec, SessionID: manifest.SessionID,
		CreatedAt: manifest.CreatedAt.UTC().Format(time.RFC3339Nano), Sequence: sequence,
		Title: projection.Title, ModelRef: projection.ModelRef, ModelIdentity: projection.ModelIdentity,
	}
	for _, turn := range projection.Turns {
		if turn.EndSequence != 0 {
			metadata.Turns++
		}
	}
	metadata.Preview = firstUserPreview(projection.Messages)
	return metadata
}

// logRevision identifies the durable file revision a cache entry describes.
// Catalog listing deliberately uses metadata only; it never opens or samples
// event bodies. Session incarnation, size, and nanosecond mtime fence a stale
// cache, while durable writers refresh the cache after every drained prefix.
type logRevision struct {
	Size      int64
	ModTimeNS int64
	Identity  string
	Exists    bool
}

// revisionOfLog inspects the manifest-selected event log without reading its body.
func revisionOfLog(dir string) (logRevision, error) {
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if os.IsNotExist(err) {
		return logRevision{}, nil
	}
	if err != nil {
		return logRevision{}, err
	}
	info, err := os.Stat(logPathForManifest(dir, manifest))
	if os.IsNotExist(err) {
		return logRevision{}, nil
	}
	if err != nil {
		return logRevision{}, err
	}
	return logRevision{Size: info.Size(), ModTimeNS: info.ModTime().UnixNano(), Exists: true}, nil
}

func readCatalogMetadata(cacheDir string, manifest Manifest, revision logRevision) (catalogMetadata, error) {
	data, err := os.ReadFile(catalogMetadataPath(cacheDir))
	if err != nil {
		return catalogMetadata{}, err
	}
	var metadata catalogMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return catalogMetadata{}, err
	}
	if metadata.Version != catalogMetadataVersion || metadata.Codec != Codec || metadata.SessionID != manifest.SessionID ||
		metadata.CreatedAt != manifest.CreatedAt.UTC().Format(time.RFC3339Nano) ||
		metadata.LogSize != revision.Size || metadata.LogModTimeNS != revision.ModTimeNS || metadata.LogIdentity != revision.Identity {
		return catalogMetadata{}, errors.New("session: catalog metadata cache is stale")
	}
	return metadata, nil
}

func writeCatalogMetadata(cacheDir string, metadata catalogMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(catalogMetadataPath(cacheDir), append(data, '\n'), 0o600)
}

func rebuildCatalogMetadata(ctx context.Context, handle eventPageReader, cacheDir, sessionDir string, manifest Manifest) error {
	metadata, err := reduceCatalogMetadata(ctx, handle, manifest)
	if err != nil {
		return err
	}
	return writeCatalogMetadataForSession(cacheDir, sessionDir, metadata)
}

func reduceCatalogMetadata(ctx context.Context, handle eventPageReader, manifest Manifest) (catalogMetadata, error) {
	projection := Projection{}
	var cursor uint64
	for {
		page, err := handle.Read(ctx, cursor, 1000)
		if err != nil {
			return catalogMetadata{}, err
		}
		for _, commit := range page.Commits {
			if err := applyProjectionCommit(&projection, commit); err != nil {
				return catalogMetadata{}, err
			}
		}
		if !page.Truncated {
			break
		}
		if page.Next <= cursor {
			return catalogMetadata{}, ErrDamagedStore
		}
		cursor = page.Next
	}
	return metadataFromProjection(manifest, projection.CommittedSequence, projection), nil
}

// writeCatalogMetadataForSession stamps the cache with the exact durable bytes
// it describes. Every writer of the cache goes through here so a reader can
// validate it using file metadata without reading event bodies.
func writeCatalogMetadataForSession(cacheDir, sessionDir string, metadata catalogMetadata) error {
	revision, err := revisionOfLog(sessionDir)
	if err != nil {
		return err
	}
	metadata.LogSize, metadata.LogModTimeNS, metadata.LogIdentity = revision.Size, revision.ModTimeNS, revision.Identity
	return writeCatalogMetadata(cacheDir, metadata)
}
