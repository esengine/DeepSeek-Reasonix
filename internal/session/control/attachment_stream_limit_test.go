package control

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
)

func TestAttachmentStreamLimitBoundaryAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int64
		size   int
		refuse bool
	}{
		{"exact", 8, 8, false}, {"one over", 8, 9, true},
		{"unlimited", 0, 16, false}, {"largest limit", math.MaxInt64, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ref, err := SaveAttachmentStreamInRoot(t.Context(), root, "test.apk", bytes.NewReader(make([]byte, tc.size)), config.AttachmentLimit{Bytes: tc.limit, Source: "user"})
			if tc.refuse {
				var limit *AttachmentStreamLimitError
				if !errors.As(err, &limit) || limit.LimitBytes != tc.limit || limit.LimitSource != "user" || ref != "" {
					t.Fatalf("ref=%q error=%v", ref, err)
				}
				entries, readErr := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				for _, entry := range entries {
					if entry.Name() != ".gitignore" {
						t.Fatalf("left partial %s", entry.Name())
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(root, ref))
			if err != nil || info.Size() != int64(tc.size) {
				t.Fatalf("file=%v error=%v", info, err)
			}
		})
	}
}

func TestAttachmentStreamDefaultLimitCountsOnlyThroughFirstExcessByte(t *testing.T) {
	reader := &countedAttachmentBytes{}
	n, err := copyAttachmentStream(io.Discard, reader, config.AttachmentLimit{Bytes: config.DefaultAttachmentStreamBytes, Source: "default"})
	var limit *AttachmentStreamLimitError
	if !errors.As(err, &limit) || n != 1<<30 || reader.count != n+1 || limit.LimitSource != "default" {
		t.Fatalf("written=%d read=%d error=%v", n, reader.count, err)
	}
}

type countedAttachmentBytes struct{ count int64 }

func (r *countedAttachmentBytes) Read(p []byte) (int, error) {
	clear(p)
	r.count += int64(len(p))
	return len(p), nil
}
