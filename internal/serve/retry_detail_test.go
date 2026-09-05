package serve

import (
	"strings"
	"testing"
)

func TestServeIndexShowsRetryDetails(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		"'retrying_detail': 'Retrying ({attempt}/{max}) · {reason} · {delay}s...'",
		"'retrying_detail': '正在重试 ({attempt}/{max}) · {reason} · {delay} 秒...'",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("serve index missing retry detail %q", want)
		}
	}
}
