package installsource

import (
	"crypto/sha256"
	"encoding/hex"

	"reasonix/internal/ext/pluginpkg"
)

// FramePlanInfo is one frame document a plugin package ships, as the plan
// shows it: the digest is of the bytes that will be installed.
type FramePlanInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Entry  string `json:"entry"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
	Error  string `json:"error,omitempty"`
}

func framePlanInfo(pkg pluginpkg.Package) []FramePlanInfo {
	if len(pkg.Manifest.Frames) == 0 {
		return nil
	}
	out := make([]FramePlanInfo, 0, len(pkg.Manifest.Frames))
	for _, f := range pkg.Manifest.Frames {
		info := FramePlanInfo{ID: f.ID, Title: f.Title, Entry: f.Entry}
		doc, err := pkg.FrameDocument(f.ID)
		if err != nil {
			info.Error = err.Error()
		} else {
			sum := sha256.Sum256(doc)
			info.Bytes, info.SHA256 = int64(len(doc)), hex.EncodeToString(sum[:])
		}
		out = append(out, info)
	}
	return out
}

func (p *previewer) frames(in []FramePlanInfo) []FramePlanInfo {
	if in == nil {
		return nil
	}
	out := make([]FramePlanInfo, len(in))
	for i, f := range in {
		out[i] = FramePlanInfo{ID: p.identity(f.ID), Title: p.prose(f.Title), Entry: p.locator(f.Entry), Bytes: f.Bytes, SHA256: f.SHA256, Error: p.prose(f.Error)}
	}
	return out
}
