//go:build windows

package main

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier     { return hotkey.ModAlt }
func hotkeyMetaModifier() hotkey.Modifier    { return hotkey.ModWin }
func hotkeyPrimaryModifier() hotkey.Modifier { return hotkey.ModCtrl }
