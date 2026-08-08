package main

import "golang.design/x/hotkey"

func hotkeyLetterKey(r rune) hotkey.Key {
	switch r {
	case 'a':
		return hotkey.KeyA
	case 'b':
		return hotkey.KeyB
	case 'c':
		return hotkey.KeyC
	case 'd':
		return hotkey.KeyD
	case 'e':
		return hotkey.KeyE
	case 'f':
		return hotkey.KeyF
	case 'g':
		return hotkey.KeyG
	case 'h':
		return hotkey.KeyH
	case 'i':
		return hotkey.KeyI
	case 'j':
		return hotkey.KeyJ
	case 'k':
		return hotkey.KeyK
	case 'l':
		return hotkey.KeyL
	case 'm':
		return hotkey.KeyM
	case 'n':
		return hotkey.KeyN
	case 'o':
		return hotkey.KeyO
	case 'p':
		return hotkey.KeyP
	case 'q':
		return hotkey.KeyQ
	case 'r':
		return hotkey.KeyR
	case 's':
		return hotkey.KeyS
	case 't':
		return hotkey.KeyT
	case 'u':
		return hotkey.KeyU
	case 'v':
		return hotkey.KeyV
	case 'w':
		return hotkey.KeyW
	case 'x':
		return hotkey.KeyX
	case 'y':
		return hotkey.KeyY
	case 'z':
		return hotkey.KeyZ
	default:
		return 0
	}
}

func hotkeyDigitKey(r rune) hotkey.Key {
	switch r {
	case '0':
		return hotkey.Key0
	case '1':
		return hotkey.Key1
	case '2':
		return hotkey.Key2
	case '3':
		return hotkey.Key3
	case '4':
		return hotkey.Key4
	case '5':
		return hotkey.Key5
	case '6':
		return hotkey.Key6
	case '7':
		return hotkey.Key7
	case '8':
		return hotkey.Key8
	case '9':
		return hotkey.Key9
	default:
		return 0
	}
}

func hotkeyFunctionKey(n int) (hotkey.Key, bool) {
	switch n {
	case 1:
		return hotkey.KeyF1, true
	case 2:
		return hotkey.KeyF2, true
	case 3:
		return hotkey.KeyF3, true
	case 4:
		return hotkey.KeyF4, true
	case 5:
		return hotkey.KeyF5, true
	case 6:
		return hotkey.KeyF6, true
	case 7:
		return hotkey.KeyF7, true
	case 8:
		return hotkey.KeyF8, true
	case 9:
		return hotkey.KeyF9, true
	case 10:
		return hotkey.KeyF10, true
	case 11:
		return hotkey.KeyF11, true
	case 12:
		return hotkey.KeyF12, true
	default:
		return 0, false
	}
}
