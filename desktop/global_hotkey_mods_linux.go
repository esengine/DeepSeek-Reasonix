//go:build linux

package main

import "golang.design/x/hotkey"

// X11: Alt ≈ Mod1, Super/Meta ≈ Mod4.
func hotkeyAltModifier() hotkey.Modifier     { return hotkey.Mod1 }
func hotkeyMetaModifier() hotkey.Modifier    { return hotkey.Mod4 }
func hotkeyPrimaryModifier() hotkey.Modifier { return hotkey.ModCtrl }
