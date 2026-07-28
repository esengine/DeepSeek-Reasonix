//go:build !darwin && !windows && !linux

package main

import "errors"

func platformEmbedAvailable() bool { return false }

//nolint:unused // only called from darwin emitEmbedBrowserState
func platformEmbedEngineName() string { return "none" }

func platformEmbedShow() error { return errors.New("embedded browser unavailable") }

func platformEmbedHide() {}

func platformEmbedDestroy() {}

func platformEmbedSetBounds(_ EmbedBrowserBounds) {}

func platformEmbedNavigate(_ string) error { return errors.New("embedded browser unavailable") }

func platformEmbedReload() {}

func platformEmbedGoBack() {}

func platformEmbedGoForward() {}

func platformEmbedSetZoom(_ float64) {}

func platformEmbedSnapshotPNG() (string, error) {
	return "", errors.New("embedded browser unavailable")
}

func platformEmbedSetPickMode(_ bool, _, _ string) {}
