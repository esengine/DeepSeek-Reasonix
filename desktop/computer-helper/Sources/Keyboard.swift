import ApplicationServices
import Carbon.HIToolbox

// Keyboard input goes to one process, never to whatever is frontmost, so the
// person's own typing elsewhere is not interleaved with it.
enum Keyboard {
    static let named: [String: Int] = [
        "enter": kVK_Return, "return": kVK_Return, "tab": kVK_Tab, "escape": kVK_Escape, "space": kVK_Space,
        "backspace": kVK_Delete, "delete": kVK_ForwardDelete,
        "arrowleft": kVK_LeftArrow, "arrowright": kVK_RightArrow, "arrowup": kVK_UpArrow, "arrowdown": kVK_DownArrow,
        "home": kVK_Home, "end": kVK_End, "pageup": kVK_PageUp, "pagedown": kVK_PageDown, "insert": kVK_Help,
        "a": kVK_ANSI_A, "b": kVK_ANSI_B, "c": kVK_ANSI_C, "d": kVK_ANSI_D, "e": kVK_ANSI_E, "f": kVK_ANSI_F,
        "g": kVK_ANSI_G, "h": kVK_ANSI_H, "i": kVK_ANSI_I, "j": kVK_ANSI_J, "k": kVK_ANSI_K, "l": kVK_ANSI_L,
        "m": kVK_ANSI_M, "n": kVK_ANSI_N, "o": kVK_ANSI_O, "p": kVK_ANSI_P, "q": kVK_ANSI_Q, "r": kVK_ANSI_R,
        "s": kVK_ANSI_S, "t": kVK_ANSI_T, "u": kVK_ANSI_U, "v": kVK_ANSI_V, "w": kVK_ANSI_W, "x": kVK_ANSI_X,
        "y": kVK_ANSI_Y, "z": kVK_ANSI_Z,
        "0": kVK_ANSI_0, "1": kVK_ANSI_1, "2": kVK_ANSI_2, "3": kVK_ANSI_3, "4": kVK_ANSI_4,
        "5": kVK_ANSI_5, "6": kVK_ANSI_6, "7": kVK_ANSI_7, "8": kVK_ANSI_8, "9": kVK_ANSI_9,
        "f1": kVK_F1, "f2": kVK_F2, "f3": kVK_F3, "f4": kVK_F4, "f5": kVK_F5, "f6": kVK_F6,
        "f7": kVK_F7, "f8": kVK_F8, "f9": kVK_F9, "f10": kVK_F10, "f11": kVK_F11, "f12": kVK_F12,
        "minus": kVK_ANSI_Minus, "equal": kVK_ANSI_Equal, "comma": kVK_ANSI_Comma, "period": kVK_ANSI_Period,
        "slash": kVK_ANSI_Slash, "semicolon": kVK_ANSI_Semicolon, "quote": kVK_ANSI_Quote,
        "bracketleft": kVK_ANSI_LeftBracket, "bracketright": kVK_ANSI_RightBracket,
        "backslash": kVK_ANSI_Backslash, "backquote": kVK_ANSI_Grave,
    ]
    static let modifiers: [String: CGEventFlags] = [
        "shift": .maskShift, "control": .maskControl, "ctrl": .maskControl,
        "alt": .maskAlternate, "option": .maskAlternate, "meta": .maskCommand, "cmd": .maskCommand,
    ]

    static func type(pid: pid_t, text: String) throws -> JSON {
        try Permissions.requireAccessibility()
        let source = CGEventSource(stateID: .privateState)
        let units = Array(text.utf16)
        var at = 0
        while at < units.count {
            let chunk = Array(units[at..<min(at + 16, units.count)])
            at += chunk.count
            for down in [true, false] {
                guard let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: down) else { continue }
                chunk.withUnsafeBufferPointer { event.keyboardSetUnicodeString(stringLength: chunk.count, unicodeString: $0.baseAddress) }
                event.postToPid(pid)
            }
        }
        return [:]
    }

    // hold keeps a key down, which is what a game or a scrubbing control reads
    // rather than a press.
    static func hold(pid: pid_t, chord: String, seconds: Double) throws -> JSON {
        let (code, flags) = try resolve(chord)
        let source = CGEventSource(stateID: .privateState)
        if let down = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(code), keyDown: true) {
            down.flags = flags
            down.postToPid(pid)
        }
        Thread.sleep(forTimeInterval: min(max(seconds, 0), 30))
        if let up = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(code), keyDown: false) {
            up.flags = flags
            up.postToPid(pid)
        }
        return [:]
    }

    // resolve reads a chord like "meta+s" into the key and the modifiers held
    // with it, and says what the vocabulary is when it cannot.
    static func resolve(_ chord: String) throws -> (Int, CGEventFlags) {
        try Permissions.requireAccessibility()
        let parts = chord.split(separator: "+").map { $0.trimmingCharacters(in: .whitespaces).lowercased() }
        guard let last = parts.last, let code = named[last] else {
            throw Failure(code: "computer.bad_step",
                          message: "\(chord) is not a key this can press. It presses named keys — enter, tab, escape, space, backspace, delete, insert, the arrows, home, end, pageup, pagedown, f1 to f12, a letter, a digit, or minus, equal, comma, period, slash, semicolon, quote, bracketleft, bracketright, backslash, backquote — with the modifiers shift, control, alt and meta. Text goes through the type action")
        }
        var flags = CGEventFlags()
        for part in parts.dropLast() {
            guard let flag = modifiers[part] else {
                throw Failure(code: "computer.bad_step", message: "\(part) is not a modifier; use shift, control, alt or meta")
            }
            flags.insert(flag)
        }
        return (code, flags)
    }

    static func press(pid: pid_t, chord: String, times: Int) throws -> JSON {
        let (code, flags) = try resolve(chord)
        let source = CGEventSource(stateID: .privateState)
        for _ in 0..<max(times, 1) {
            for down in [true, false] {
                guard let event = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(code), keyDown: down) else { continue }
                event.flags = flags
                event.postToPid(pid)
            }
        }
        return [:]
    }
}
