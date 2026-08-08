//go:build darwin

package main

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier     { return hotkey.ModOption }
func hotkeyMetaModifier() hotkey.Modifier    { return hotkey.ModCmd }
func hotkeyPrimaryModifier() hotkey.Modifier { return hotkey.ModCmd }
