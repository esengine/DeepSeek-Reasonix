package sessionstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

// NativeArchive includes resolved resources; importing it grants no execution authority.
type NativeArchive struct {
	Format  string             `json:"format"`
	Version int                `json:"version"`
	Records []provider.Message `json:"records"`
}

const NativeArchiveFormat = "reasonix-native-session"
const NativeArchiveVersion = 2
const MaxNativeArchiveBytes = 192 << 20

func ExportNativeArchive(path string) ([]byte, error) {
	s, err := LoadSession(path)
	if err != nil {
		return nil, err
	}
	archive := NativeArchive{Format: NativeArchiveFormat, Version: NativeArchiveVersion, Records: s.Snapshot()}
	data, err := json.Marshal(archive)
	if len(data) > MaxNativeArchiveBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	return data, err
}

func DecodeNativeArchive(data []byte) (*Session, error) {
	if len(data) > MaxNativeArchiveBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	var archive NativeArchive
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archive); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid native archive: trailing data")
	}
	if archive.Format != NativeArchiveFormat || archive.Version != NativeArchiveVersion {
		return nil, fmt.Errorf("unsupported native archive version %d", archive.Version)
	}
	if len(archive.Records) > sessionEventReplayMaxMessages {
		return nil, ErrSessionReplayLimitExceeded
	}
	var binding *agentcomposition.Binding
	for i, m := range archive.Records {
		if m.AgentComposition == nil {
			continue
		}
		if i != 0 || m.Role != provider.RoleSystem || binding != nil {
			return nil, agentcomposition.ErrInvalid
		}
		binding = m.AgentComposition
		if err := binding.Validate(); err != nil {
			return nil, err
		}
	}
	s := NewSession("")
	s.Messages = archive.Records
	if !s.HasSystemMessage() {
		return nil, fmt.Errorf("native archive has no system message")
	}
	if binding != nil && s.HasContent() && !binding.Started {
		return nil, fmt.Errorf("%w: admitted archive is unlocked", agentcomposition.ErrInvalid)
	}
	return s, nil
}
