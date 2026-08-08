package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.design/x/hotkey"
)

func platformRegisterGlobalHotkey(ctx context.Context, binding string, onTrigger func()) (func(), error) {
	mods, key, err := parseHotkeyBinding(binding)
	if err != nil {
		return nil, err
	}
	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return nil, fmt.Errorf("OS refused binding %q: %w", binding, err)
	}
	unregistered := make(chan struct{})
	go func() {
		defer close(unregistered)
		defer func() {
			if err := hk.Unregister(); err != nil {
				slog.Debug("desktop: unregister global hotkey", "err", err)
			}
		}()
		keydown := hk.Keydown()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-keydown:
				if !ok {
					return
				}
				if onTrigger != nil {
					onTrigger()
				}
			}
		}
	}()
	return func() {
		<-unregistered
	}, nil
}

func parseHotkeyBinding(binding string) ([]hotkey.Modifier, hotkey.Key, error) {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(binding)), func(r rune) bool {
		return r == '+' || r == '-' || r == ' ' || r == '_'
	})
	if len(parts) == 0 {
		return nil, 0, fmt.Errorf("empty hotkey")
	}
	var (
		mods []hotkey.Modifier
		key  string
	)
	for _, part := range parts {
		switch part {
		case "ctrl", "control", "controlkey":
			mods = append(mods, hotkey.ModCtrl)
		case "shift":
			mods = append(mods, hotkey.ModShift)
		case "alt", "option", "opt":
			mods = append(mods, hotkeyAltModifier())
		case "cmd", "command", "meta", "super", "win", "windows":
			mods = append(mods, hotkeyMetaModifier())
		case "mod", "primary":
			mods = append(mods, hotkeyPrimaryModifier())
		default:
			key = part
		}
	}
	if key == "" {
		return nil, 0, fmt.Errorf("hotkey %q is missing a key", binding)
	}
	if len(mods) == 0 {
		return nil, 0, fmt.Errorf("hotkey %q needs at least one modifier", binding)
	}
	k, ok := hotkeyKey(key)
	if !ok {
		return nil, 0, fmt.Errorf("unsupported hotkey key %q", key)
	}
	return mods, k, nil
}

func parseHotkeyKeyName(name string) (hotkey.Key, bool) {
	switch name {
	case "space", " ":
		return hotkey.KeySpace, true
	case "enter", "return":
		return hotkey.KeyReturn, true
	case "tab":
		return hotkey.KeyTab, true
	case "escape", "esc":
		return hotkey.KeyEscape, true
	}
	if len(name) == 1 {
		r := rune(name[0])
		if r >= 'a' && r <= 'z' {
			return hotkeyLetterKey(r), true
		}
		if r >= '0' && r <= '9' {
			return hotkeyDigitKey(r), true
		}
	}
	if len(name) >= 2 && name[0] == 'f' {
		n := 0
		ok := true
		for _, r := range name[1:] {
			if !unicode.IsDigit(r) {
				ok = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if ok {
			return hotkeyFunctionKey(n)
		}
	}
	return 0, false
}

func hotkeyKey(name string) (hotkey.Key, bool) {
	return parseHotkeyKeyName(name)
}

func windowLooksBackgrounded(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	return runtime.WindowIsMinimised(ctx)
}
