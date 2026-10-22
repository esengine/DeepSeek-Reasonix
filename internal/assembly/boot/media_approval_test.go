package boot

import (
	"testing"

	"reasonix/internal/safety/permission"
)

const videoCall = `{"path":"clip.mp4"}`

// Decoding a stranger's media is a larger attack surface than reading a file,
// and the OS sandbox is what bounds it where there is one. Where there is none
// the first use is the user's to approve.
func TestMediaDecodersAskFirstWhereNothingSandboxesThem(t *testing.T) {
	for _, tool := range []string{"read_video", "check_video"} {
		for name, c := range map[string]struct {
			allow, ask, deny []string
			sandboxed        bool
			want             permission.Decision
		}{
			"no sandbox, no rule":          {sandboxed: false, want: permission.Ask},
			"sandboxed":                    {sandboxed: true, want: permission.Allow},
			"no sandbox, user allows it":   {allow: []string{tool}, want: permission.Allow},
			"no sandbox, user denies it":   {deny: []string{tool}, want: permission.Deny},
			"no sandbox, user asks itself": {ask: []string{tool}, want: permission.Ask},
		} {
			t.Run(tool+"/"+name, func(t *testing.T) {
				ask := askWithUnsandboxedMedia(c.allow, c.ask, c.deny, c.sandboxed)
				got := permission.New("ask", c.allow, ask, c.deny).Decide(tool, true, []byte(videoCall))
				if got != c.want {
					t.Errorf("decision %v, want %v", got, c.want)
				}
			})
		}
	}
}

func TestTheDefaultMediaRuleDoesNotTouchOtherReads(t *testing.T) {
	ask := askWithUnsandboxedMedia(nil, nil, nil, false)
	p := permission.New("ask", nil, ask, nil)
	for _, name := range []string{"read_file", "grep", "ls"} {
		if got := p.Decide(name, true, []byte(`{"path":"a"}`)); got != permission.Allow {
			t.Errorf("%s decision %v, want Allow", name, got)
		}
	}
}

func TestTheDefaultMediaRuleDoesNotMutateTheConfigList(t *testing.T) {
	ask := []string{"bash"}
	_ = askWithUnsandboxedMedia(nil, ask, nil, false)
	if len(ask) != 1 {
		t.Errorf("the caller's slice was changed: %v", ask)
	}
}
