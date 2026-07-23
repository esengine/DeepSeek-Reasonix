package cli

import "testing"

func TestResolveExistingPastedPathWindowsCandidates(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		exists []string
		want   string
		ok     bool
	}{
		{
			name:   "native windows path wins",
			input:  `C:\Users\me\shot.png`,
			exists: []string{`C:\Users\me\shot.png`},
			want:   `C:\Users\me\shot.png`,
			ok:     true,
		},
		{
			name:   "git bash slash path",
			input:  `C:/Users/me/My\ Shot.png`,
			exists: []string{`C:/Users/me/My Shot.png`},
			want:   `C:/Users/me/My Shot.png`,
			ok:     true,
		},
		{
			name:   "git bash backslash path",
			input:  `C:\Users\me\My\ Shot.png`,
			exists: []string{`C:\Users\me\My Shot.png`},
			want:   `C:\Users\me\My Shot.png`,
			ok:     true,
		},
		{
			name:   "msys drive path",
			input:  `/c/Users/me/My\ Shot.png`,
			exists: []string{`C:/Users/me/My Shot.png`},
			want:   `C:/Users/me/My Shot.png`,
			ok:     true,
		},
		{
			name:   "wsl mounted drive path",
			input:  `/mnt/c/Users/me/My\ Shot.png`,
			exists: []string{`C:/Users/me/My Shot.png`},
			want:   `C:/Users/me/My Shot.png`,
			ok:     true,
		},
		{
			name:   "cygwin drive path",
			input:  `/cygdrive/c/Users/me/My\ Shot.png`,
			exists: []string{`C:/Users/me/My Shot.png`},
			want:   `C:/Users/me/My Shot.png`,
			ok:     true,
		},
		{
			name:   "unc path",
			input:  `//server/share/My\ Shot.png`,
			exists: []string{`//server/share/My Shot.png`},
			want:   `//server/share/My Shot.png`,
			ok:     true,
		},
		{
			name:   "unc backslash path",
			input:  `\\server\share\My\ Shot.png`,
			exists: []string{`\\server\share\My Shot.png`},
			want:   `\\server\share\My Shot.png`,
			ok:     true,
		},
		{
			name:   "extended-length path",
			input:  `\\?\C:\Users\me\My\ Shot.png`,
			exists: []string{`\\?\C:\Users\me\My Shot.png`},
			want:   `\\?\C:\Users\me\My Shot.png`,
			ok:     true,
		},
		{
			name:  "native interpretation precedes shell fallback",
			input: `C:\work\(draft)\shot.png`,
			exists: []string{
				`C:\work\(draft)\shot.png`,
				`C:\work(draft)\shot.png`,
			},
			want: `C:\work\(draft)\shot.png`,
			ok:   true,
		},
		{
			name:  "missing candidates rejected",
			input: `C:/Users/me/Missing\ Shot.png`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := make(map[string]bool, len(tt.exists))
			for _, path := range tt.exists {
				existing[path] = true
			}
			got, ok := resolveExistingPastedPath(tt.input, "windows", false, func(path string) bool {
				return existing[path]
			})
			if ok != tt.ok || got != tt.want {
				t.Fatalf("resolveExistingPastedPath(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestPastedImageSourcesUsesStaticShellFields(t *testing.T) {
	input := `C:/Users/me/first" image".png C:/Users/me/second\ image.jpg`
	got, ok := pastedImageSourcesForOS(input, "windows")
	if !ok {
		t.Fatal("pastedImageSourcesForOS rejected static shell paths")
	}
	want := []pastedImageSource{
		{value: "C:/Users/me/first image.png", shellDecoded: true},
		{value: "C:/Users/me/second image.jpg", shellDecoded: true},
	}
	if len(got) != len(want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("source[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
