package builtin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/tool"
)

func contentDigest(content string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(content))) }
func resolvedWritePath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	if filepath.Dir(path) == path {
		return "", fmt.Errorf("cannot resolve write root")
	}
	parent, err := resolvedWritePath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
func (s editSource) recordWrite(ctx context.Context, path, content, route string, overlay FileOverlay) error {
	if !tool.HasWriteIntentHook(ctx) {
		return nil
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	resolved, err := resolvedWritePath(path)
	if err != nil {
		return err
	}
	transport := ""
	if id, ok := overlay.(interface{ RecoveryIdentity() string }); ok {
		transport = id.RecoveryIdentity()
	}
	return tool.RecordWriteIntent(ctx, tool.FileWriteIntent{TransportID: transport, Version: 1, Path: path, Host: host, Route: route, ResolvedPath: resolved, Before: contentDigest(s.content), After: contentDigest(content), Encoding: fmt.Sprint(s.enc), Existed: s.id.existed})
}
func verifyFileWrite(ctx context.Context, overlay FileOverlay, intent tool.FileWriteIntent) tool.WriteVerification {
	host, err := os.Hostname()
	if err != nil || intent.Version != 1 || host != intent.Host || !filepath.IsAbs(intent.Path) {
		return tool.WriteUnknown
	}
	resolved, err := resolvedWritePath(intent.Path)
	if err != nil || resolved != intent.ResolvedPath {
		return tool.WriteUnknown
	}
	var content, encoding string
	switch intent.Route {
	case "overlay":
		// Without a stable transport identity, an overlay cannot prove that it
		// still addresses the same editor/remote host after restart.
		identity, ok := overlay.(interface{ RecoveryIdentity() string })
		if !ok || intent.TransportID == "" || identity.RecoveryIdentity() != intent.TransportID {
			return tool.WriteUnknown
		}
		value, ok := overlay.ReadTextFile(ctx, intent.Path)
		if !ok {
			return tool.WriteUnknown
		}
		content, encoding = value, intent.Encoding
	case "disk":
		value, enc, readErr := readFileEncoded(intent.Path)
		if readErr != nil {
			return tool.WriteUnknown
		}
		content, encoding = value, fmt.Sprint(enc)
	default:
		return tool.WriteUnknown
	}
	if encoding != intent.Encoding {
		return tool.WriteConflict
	}
	digest := contentDigest(content)
	if digest == intent.After {
		return tool.WriteSatisfied
	}
	if digest == intent.Before {
		return tool.WriteUnchanged
	}
	return tool.WriteConflict
}

func (w writeFile) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, w.rootSet, w.roots), w.guard, w.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, w.overlay, intent)
}

func (e editFile) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, e.rootSet, e.roots), e.guard, e.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, e.overlay, intent)
}

func (m multiEdit) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, m.rootSet, m.roots), m.guard, m.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, m.overlay, intent)
}

func (n notebookEdit) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, n.rootSet, n.roots), n.guard, n.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, n.overlay, intent)
}

func (d deleteRange) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, d.rootSet, d.roots), d.guard, d.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, d.overlay, intent)
}

func (d deleteSymbol) VerifyWrite(ctx context.Context, intent tool.FileWriteIntent) tool.WriteVerification {
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, d.rootSet, d.roots), d.guard, d.managed, intent.Path); err != nil {
		return tool.WriteUnknown
	}
	return verifyFileWrite(ctx, d.overlay, intent)
}
