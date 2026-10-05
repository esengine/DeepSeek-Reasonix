import AppKit

// The agent's own pointer: drawn over everything, taking no clicks and no
// focus, so a person can see where it acts without it touching theirs.
final class VirtualCursor {
    private let panel: NSPanel
    private var hideTimer: Timer?
    private static let size = NSSize(width: 120, height: 44)

    init() {
        panel = NSPanel(contentRect: NSRect(origin: .zero, size: Self.size), styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: true)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.ignoresMouseEvents = true
        panel.level = .screenSaver
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        panel.contentView = CursorView(frame: NSRect(origin: .zero, size: Self.size))
        // Escape while the agent's cursor is on screen is the person saying stop.
        // Any other time it is their own keystroke and not this process's business.
        NSEvent.addGlobalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self, event.keyCode == 53, self.panel.isVisible else { return }
            reply(["event": "stop"])
            self.hide()
        }
    }

    // move glides the pointer to a point given in global top-left coordinates,
    // the ones accessibility and window lists use.
    func move(to point: CGPoint) {
        guard let primary = NSScreen.screens.first else { return }
        let origin = NSPoint(x: point.x - 2, y: primary.frame.height - point.y - Self.size.height + 2)
        if !panel.isVisible {
            panel.setFrameOrigin(origin)
            panel.orderFrontRegardless()
        } else {
            NSAnimationContext.runAnimationGroup { context in
                context.duration = 0.25
                panel.animator().setFrameOrigin(origin)
            }
        }
        RunLoop.main.run(until: Date().addingTimeInterval(0.28))
        hideTimer?.invalidate()
        hideTimer = Timer.scheduledTimer(withTimeInterval: 4, repeats: false) { [weak self] _ in self?.hide() }
    }

    func hide() {
        hideTimer?.invalidate()
        panel.orderOut(nil)
    }
}

private final class CursorView: NSView {
    override func draw(_ dirtyRect: NSRect) {
        let top = bounds.height
        let arrow = NSBezierPath()
        arrow.move(to: NSPoint(x: 2, y: top - 2))
        arrow.line(to: NSPoint(x: 2, y: top - 22))
        arrow.line(to: NSPoint(x: 7.5, y: top - 17))
        arrow.line(to: NSPoint(x: 11, y: top - 25))
        arrow.line(to: NSPoint(x: 14, y: top - 23.5))
        arrow.line(to: NSPoint(x: 10.5, y: top - 16))
        arrow.line(to: NSPoint(x: 17, y: top - 16))
        arrow.close()
        NSColor.systemIndigo.setFill()
        arrow.fill()
        NSColor.white.setStroke()
        arrow.lineWidth = 1.5
        arrow.stroke()

        let label = "Reasonix" as NSString
        let attrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 11, weight: .semibold), .foregroundColor: NSColor.white]
        let textSize = label.size(withAttributes: attrs)
        let pill = NSRect(x: 16, y: top - 40, width: textSize.width + 12, height: textSize.height + 4)
        NSColor.systemIndigo.setFill()
        NSBezierPath(roundedRect: pill, xRadius: pill.height / 2, yRadius: pill.height / 2).fill()
        label.draw(at: NSPoint(x: pill.minX + 6, y: pill.minY + 2), withAttributes: attrs)
    }
}
