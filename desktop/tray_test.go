package main

import "testing"

func TestTrayMenuLabelsFollowLocale(t *testing.T) {
	zh := trayMenuLabels("zh")
	if zh.openTitle != "打开" || zh.quitTitle != "退出" {
		t.Fatalf("zh labels = %#v", zh)
	}

	en := trayMenuLabels("en")
	if en.openTitle != "Open" || en.quitTitle != "Quit" {
		t.Fatalf("en labels = %#v", en)
	}

	other := trayMenuLabels("fr")
	if other.openTitle != en.openTitle || other.quitTitle != en.quitTitle {
		t.Fatalf("unknown locale should fall back to English, got %#v", other)
	}
}
