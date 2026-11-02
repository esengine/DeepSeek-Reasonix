package sessionstore

import (
	"testing"

	"reasonix/internal/contract/provider"
)

func saidLine(text string) provider.Message {
	return provider.Message{Role: provider.RoleUser, RawContent: text}
}

func TestStripAttachmentTokensKeepsWhatThePersonWrote(t *testing.T) {
	const image = "@.reasonix/attachments/clipboard-20260101-000000.000000-000001.png"
	const other = "@.reasonix/attachments/clipboard-20260101-000000.000000-000002.png"
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"leading token", image + " 请看这张图片", "请看这张图片"},
		{"between words", "请看 " + image + " 这张图", "请看 这张图"},
		{"trailing token", "看图 " + image, "看图"},
		{"token alone", image, ""},
		{"two tokens one line", image + " " + other + " 对比", "对比"},
		{"one token per line", image + "\n" + other + "\n说明", "说明"},
		{"plain text", "把 a@b.com 发我", "把 a@b.com 发我"},
		{"similar name outside the store", "@.reasonix/attachments-notes.md 看一下", "@.reasonix/attachments-notes.md 看一下"},
		{"no token", "你好", "你好"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripAttachmentTokens(tc.in); got != tc.want {
				t.Fatalf("StripAttachmentTokens(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsAttachmentTokenMatchesTheStorePrefix(t *testing.T) {
	if !IsAttachmentToken(".reasonix/attachments/clipboard-x.png") {
		t.Fatal("a stored attachment token was not recognised")
	}
	for _, tok := range []string{".reasonix/attachments-backup/x.png", "notes/x.png", "./.reasonix/attachments/x.png"} {
		if IsAttachmentToken(tok) {
			t.Fatalf("%q was taken for an attachment token", tok)
		}
	}
}

// A message that is nothing but a picture still names its attachment: the
// display shows the picture, while the line the model was given keeps the token
// turn identity and attachment lookup read.
func TestUserDisplayTextDropsOnlyTheTokens(t *testing.T) {
	const image = "@.reasonix/attachments/clipboard-20260101-000000.000000-000001.png"
	msg := saidLine(image + " 这是什么")
	if got := UserDisplayText(msg); got != "这是什么" {
		t.Fatalf("display text = %q", got)
	}
	if got := UserMessageText(msg); got != image+" 这是什么" {
		t.Fatalf("message text = %q, want the line the model was given", got)
	}
	if got := UserDisplayText(saidLine(image)); got != "" {
		t.Fatalf("an image-only turn displayed %q, want nothing", got)
	}
}

// A preview or branch title names the message, not the file the host stored it
// beside; the picture itself has no text to preview.
func TestUserPreviewTextDropsTheToken(t *testing.T) {
	const image = "@.reasonix/attachments/clipboard-20260101-000000.000000-000001.png"
	if got := UserPreviewText(image + " 这张图是什么"); got != "这张图是什么" {
		t.Fatalf("preview = %q", got)
	}
	if got := UserPreviewText(image); got != "" {
		t.Fatalf("an image-only preview = %q, want empty so callers fall back", got)
	}
}
