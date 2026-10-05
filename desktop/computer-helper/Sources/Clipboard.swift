import AppKit

enum Clipboard {
    // paste puts text where the person is typing without typing it: a long
    // value arrives at once, and an application that completes or reformats
    // what it is given sees one insertion instead of a hundred keystrokes.
    //
    // The clipboard is the person's. What was on it goes back, unless something
    // else claimed it while this ran — then theirs is the newer one and stays.
    static func paste(pid: pid_t, text: String) throws -> JSON {
        try Permissions.requireAccessibility()
        // Measured: an application that is not in front never handles the
        // keystroke, and bringing it forward is the person's screen being taken
        // over — which is answered for on its own, not folded into this.
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
            throw Failure(
                code: "computer.needs_front",
                message: "pasting needs this application in front; bring it forward with a pointer step, or use type or set_value, which work while it is behind"
            )
        }
        let board = NSPasteboard.general
        let saved = snapshot(board)
        let written = board.clearContents()
        board.setString(text, forType: .string)
        _ = try Keyboard.press(pid: pid, chord: "cmd+v", times: 1)
        // The application reads the clipboard when it gets round to handling the
        // keystroke, and giving it back before then is what hands it the
        // person's own clipboard to paste instead. So the wait is long, and it
        // is not the step's to pay: a busy application still has it a second
        // after this call has answered.
        Thread.sleep(forTimeInterval: 0.25)
        DispatchQueue.global().asyncAfter(deadline: .now() + 1.5) {
            Clipboard.restore(board, saved, writtenAt: written)
        }
        return ["pasted": text.count]
    }

    private static func snapshot(_ board: NSPasteboard) -> [[NSPasteboard.PasteboardType: Data]] {
        (board.pasteboardItems ?? []).map { item in
            var kept: [NSPasteboard.PasteboardType: Data] = [:]
            for type in item.types {
                if let data = item.data(forType: type) {
                    kept[type] = data
                }
            }
            return kept
        }
    }

    fileprivate static func restore(_ board: NSPasteboard, _ saved: [[NSPasteboard.PasteboardType: Data]], writtenAt: Int) {
        guard board.changeCount == writtenAt else { return }
        board.clearContents()
        guard !saved.isEmpty else { return }
        board.writeObjects(saved.map { kept in
            let item = NSPasteboardItem()
            for (type, data) in kept {
                item.setData(data, forType: type)
            }
            return item
        })
    }
}
