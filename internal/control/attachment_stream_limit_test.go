package control

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func TestAttachmentStreamLimitCountsActualBytesAndCleansOversize(t *testing.T) {
	for _, tc := range []struct {
		name        string
		limit, size int64
		tooLarge    bool
	}{
		{"below", 4, 3, false},
		{"exact", 4, 4, false},
		{"plus one", 4, 5, true},
		{"long stream", 4, 1 << 20, true},
		{"unlimited", 0, 20, false},
		{"maximum integer", math.MaxInt64, 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			reader := &attachmentPatternReader{remaining: tc.size}
			path, err := SaveAttachmentStreamInRoot(t.Context(), root, "app.apk", reader, config.AttachmentLimit{Bytes: tc.limit, Source: "user"})
			if tc.tooLarge {
				var limitErr *AttachmentStreamLimitError
				if !errors.As(err, &limitErr) || limitErr.LimitBytes != tc.limit || limitErr.LimitSource != "user" || path != "" {
					t.Fatalf("path=%s err=%v", path, err)
				}
				if read := tc.size - reader.remaining; read != tc.limit+1 {
					t.Fatalf("consumed=%d, want only boundary probe=%d", read, tc.limit+1)
				}
				entries, readErr := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
				if readErr != nil || len(entries) != 0 {
					t.Fatalf("partial files=%v err=%v", entries, readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil || info.Size() != tc.size {
				t.Fatalf("file=%v err=%v", info, err)
			}
		})
	}
}
