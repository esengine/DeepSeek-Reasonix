package workspacefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewReadsBytesAndRefusesEscape(t *testing.T) {
	root := t.TempDir()
	data := []byte("%PDF-1.7\n\x00binary")
	if err := os.WriteFile(filepath.Join(root, "报告.pdf"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s := Service{Primary: root}
	info, err := s.Info(context.Background(), Ref{Path: "报告.pdf"})
	if err != nil || info.Kind != "pdf" || info.Size != int64(len(data)) {
		t.Fatalf("info = %+v, %v", info, err)
	}
	got, _, err := s.Content(context.Background(), info.Ref)
	if err != nil || string(got) != string(data) {
		t.Fatalf("read = %q, %v", got, err)
	}
	for _, ref := range []Ref{{Path: "../secret"}, {Root: t.TempDir(), Path: "x"}} {
		if _, err := s.Info(context.Background(), ref); !errors.Is(err, ErrDenied) {
			t.Fatalf("escape = %v", err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err == nil {
		if _, err := s.Info(context.Background(), Ref{Path: "link/secret.txt"}); err == nil {
			t.Fatal("symlink escaped root")
		}
	}
}

func TestPreviewLimitsAndUnknownBinary(t *testing.T) {
	root := t.TempDir()
	s := Service{Primary: root}
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"big.txt", make([]byte, TextLimit+1), ErrTooLarge},
		{"unknown.bin", []byte{0, 1, 2}, ErrUnsupported},
		{"bad.txt", []byte{0xff, 0xfe}, ErrNotText},
	} {
		if err := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Content(context.Background(), Ref{Path: tc.name}); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := s.Info(context.Background(), Ref{Path: "absent"}); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
}

func TestGrantedRootAndCancellation(t *testing.T) {
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "notes.md"), []byte("# Notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Service{Primary: t.TempDir(), Extra: []string{extra}}
	ref, err := s.Resolve(filepath.Join(extra, "notes.md"))
	if err != nil || ref.Root != extra || ref.Path != "notes.md" {
		t.Fatalf("resolve = %+v, %v", ref, err)
	}
	info, err := s.Info(t.Context(), ref)
	if err != nil || !info.ReadOnly {
		t.Fatalf("grant = %+v, %v", info, err)
	}
	s.Extra = nil
	if _, err := s.Info(t.Context(), ref); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked grant = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := s.Content(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}

func TestOfficeDocumentsPreviewAsBinaryWithinTheirLimit(t *testing.T) {
	root := t.TempDir()
	zip := []byte("PK\x03\x04\x00binary office package")
	s := Service{Primary: root}
	for _, name := range []string{"报告.docx", "budget.XLSX"} {
		if err := os.WriteFile(filepath.Join(root, name), zip, 0o600); err != nil {
			t.Fatal(err)
		}
		got, info, err := s.Content(context.Background(), Ref{Path: name})
		if err != nil || info.Kind != "office" || info.Limit != OfficeLimit || string(got) != string(zip) {
			t.Fatalf("%s: kind=%q limit=%d err=%v", name, info.Kind, info.Limit, err)
		}
	}
	for _, name := range []string{"old.doc", "old.xls", "slides.pptx", "old.ppt"} {
		if kind, _ := Format(name); kind != "unsupported" {
			t.Fatalf("%s: kind %q, want unsupported", name, kind)
		}
	}
}
