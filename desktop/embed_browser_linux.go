//go:build linux

package main

import "errors"

func platformEmbedAvailable() bool {
	return false
}

//nolint:unused // only called from darwin emitEmbedBrowserState
func platformEmbedEngineName() string {
	return "webkitgtk"
}

func platformEmbedShow() error {
	return errors.New("内嵌浏览器暂未支持 Linux")
}

func platformEmbedHide() {}

func platformEmbedDestroy() {}

func platformEmbedSetBounds(_ EmbedBrowserBounds) {}

func platformEmbedNavigate(_ string) error {
	return errors.New("内嵌浏览器暂未支持 Linux")
}

func platformEmbedReload() {}

func platformEmbedGoBack() {}

func platformEmbedGoForward() {}

func platformEmbedSetZoom(_ float64) {}

func platformEmbedSnapshotPNG() (string, error) {
	return "", errors.New("内嵌浏览器暂未支持 Linux")
}

func platformEmbedSetPickMode(_ bool, _, _ string) {}
