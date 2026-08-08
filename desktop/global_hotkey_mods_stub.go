//go:build !darwin && !windows && !linux

package main

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier     { return hotkey.ModCtrl }
func hotkeyMetaModifier() hotkey.Modifier    { return hotkey.ModCtrl }
func hotkeyPrimaryModifier() hotkey.Modifier { return hotkey.ModCtrl }
