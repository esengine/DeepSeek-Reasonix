package sessionstore

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func TestUserMessagesTitleSource(t *testing.T) {
	for _, long := range []bool{false, true} {
		name := "short"
		first := "第一条用户消息"
		want := first + "\n后来" + strings.Repeat("乙", 20) + "\n最后：修复登录"
		if long {
			name = "suffix300"
			first = "旧" + strings.Repeat("甲", 320)
			want = strings.Repeat("甲", 269) + "\n后来" + strings.Repeat("乙", 20) + "\n最后：修复登录"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(testenv.TempDir(t), "session.jsonl")
			s := NewSession("system instructions")
			s.Add(provider.Message{Role: provider.RoleUser, Content: first})
			s.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant text must be excluded"})
			s.Add(provider.Message{Role: provider.RoleUser, Content: "compiled text", RawContent: "后来" + strings.Repeat("乙", 20)})
			s.Add(provider.Message{Role: provider.RoleUser, Content: "最后：修复登录"})
			s.Add(provider.Message{Role: provider.RoleUser, Content: "host text must be excluded", HostAuthored: true})
			if err := s.SaveSnapshot(path); err != nil {
				t.Fatal(err)
			}
			got, err := UserMessagesTitleSource(path)
			if err != nil || got != want {
				t.Fatalf("title source = %q, err=%v; want %q", got, err, want)
			}
		})
	}
}
