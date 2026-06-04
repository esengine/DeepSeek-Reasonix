package main

import "testing"

func TestTrayMenuLabelsFollowLocale(t *testing.T) {
	zh := trayMenuLabels("zh")
	if zh.openTitle != "打开 Reasonix" || zh.quitTitle != "退出 Reasonix" {
		t.Fatalf("zh labels = %#v", zh)
	}

	en := trayMenuLabels("en")
	if en.openTitle != "Open Reasonix" || en.quitTitle != "Quit Reasonix" {
		t.Fatalf("en labels = %#v", en)
	}

	other := trayMenuLabels("fr")
	if other.openTitle != en.openTitle || other.quitTitle != en.quitTitle {
		t.Fatalf("unknown locale should fall back to English, got %#v", other)
	}
}
