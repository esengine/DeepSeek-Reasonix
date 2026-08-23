package agent

import "testing"

func TestLooksLikePendingAction(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{text: "先读 login.php", want: true},
		{text: "立即继续执行", want: true},
		{text: "我从读 login.php 开始", want: true},
		{text: "已修改 login.php，下一步修改 functions.php。", want: true},
		{text: "Next I will update functions.php.", want: true},
		{text: "Implemented the requested change.", want: false},
		{text: "已完成请求的修改。", want: false},
		{text: "Tests were not run.", want: false},
		{text: "Optional next step: you can update functions.php.", want: false},
		{text: "如需扩展，你可以继续修改 functions.php。", want: false},
		{text: "No further action is needed.", want: false},
		{text: "The implementation is ready to use.", want: false},
		{text: "The latest update is complete. Next section: verification notes.", want: false},
	} {
		if got := looksLikePendingAction(tc.text); got != tc.want {
			t.Errorf("looksLikePendingAction(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
