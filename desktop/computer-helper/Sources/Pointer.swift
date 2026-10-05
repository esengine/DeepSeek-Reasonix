import AppKit
import CoreGraphics

// The person's own pointer, moved and clicked by the agent. Every other path in
// this helper works through accessibility and leaves the pointer where it is;
// this is the one that does not, so the kernel asks for it by name and it hands
// the pointer back where it found it.
//
// A point is acted on only while the window under it belongs to the application
// that was approved: on screen the agent has no window of its own to aim at.
enum Pointer {
    private static var parked: CGPoint?

    static func owner(of point: CGPoint) -> pid_t? {
        let list = (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]]) ?? []
        for window in list {
            guard let layer = window[kCGWindowLayer as String] as? Int, layer == 0,
                  let pid = window[kCGWindowOwnerPID as String] as? pid_t, pid != getpid(),
                  let bounds = window[kCGWindowBounds as String] as? [String: CGFloat],
                  let x = bounds["X"], let y = bounds["Y"], let w = bounds["Width"], let h = bounds["Height"] else { continue }
            if CGRect(x: x, y: y, width: w, height: h).contains(point) { return pid }
        }
        return nil
    }

    // front brings the application forward before the pointer acts on it. A
    // click into a background window is spent on activating it — macOS gives the
    // view nothing — so taking the pointer means taking the screen with it.
    static func front(_ pid: pid_t) {
        guard let app = NSRunningApplication(processIdentifier: pid), !app.isActive else { return }
        app.activate(options: [.activateAllWindows])
        Thread.sleep(forTimeInterval: 0.35)
    }

    static func check(_ pid: pid_t, _ point: CGPoint) throws {
        guard let at = owner(of: point) else {
            throw Failure(code: "computer.no_element", message: "no window is at (\(Int(point.x)), \(Int(point.y)))")
        }
        guard at == pid else {
            throw Failure(code: "computer.no_element", message: "the window at (\(Int(point.x)), \(Int(point.y))) belongs to another application")
        }
    }

    // park remembers where the person left their pointer, once per run of steps.
    static func park() {
        if parked == nil { parked = CGEvent(source: nil)?.location }
    }

    // release puts the pointer back where the person left it.
    static func release() -> JSON {
        defer { parked = nil }
        guard let home = parked else { return [:] }
        post(.mouseMoved, home, .left, 0)
        return ["returned": true]
    }

    // position is where the pointer is now, which is what a run that has moved
    // it has to be able to read back.
    static func position() -> JSON {
        let at = CGEvent(source: nil)?.location ?? .zero
        return ["x": at.x, "y": at.y]
    }

    static func move(pid: pid_t, to point: CGPoint, cursor: VirtualCursor) throws -> JSON {
        try Permissions.requireAccessibility()
        try check(pid, point)
        park()
        cursor.move(to: point)
        post(.mouseMoved, point, .left, 0)
        return [:]
    }

    static func click(pid: pid_t, at point: CGPoint, button: String, clicks: Int, cursor: VirtualCursor) throws -> JSON {
        try Permissions.requireAccessibility()
        try check(pid, point)
        park()
        front(pid)
        cursor.move(to: point)
        let (down, up, which) = try events(for: button)
        post(.mouseMoved, point, which, 0)
        for click in 1...max(clicks, 1) {
            post(down, point, which, click)
            post(up, point, which, click)
        }
        return [:]
    }

    static func drag(pid: pid_t, from: CGPoint, to: CGPoint, cursor: VirtualCursor) throws -> JSON {
        try Permissions.requireAccessibility()
        try check(pid, from)
        try check(pid, to)
        park()
        front(pid)
        cursor.move(to: from)
        post(.mouseMoved, from, .left, 0)
        post(.leftMouseDown, from, .left, 1)
        for step in 1...dragSteps {
            let at = CGFloat(step) / CGFloat(dragSteps)
            post(.leftMouseDragged, CGPoint(x: from.x + (to.x - from.x) * at, y: from.y + (to.y - from.y) * at), .left, 1)
            Thread.sleep(forTimeInterval: 0.02)
        }
        cursor.move(to: to)
        post(.leftMouseUp, to, .left, 1)
        return [:]
    }

    private static let dragSteps = 12

    private static func events(for button: String) throws -> (CGEventType, CGEventType, CGMouseButton) {
        switch button {
        case "", "left":
            return (.leftMouseDown, .leftMouseUp, .left)
        case "right":
            return (.rightMouseDown, .rightMouseUp, .right)
        case "middle":
            return (.otherMouseDown, .otherMouseUp, .center)
        default:
            throw Failure(code: "computer.bad_step", message: "\(button) is not a mouse button; use left, right or middle")
        }
    }

    private static func post(_ type: CGEventType, _ point: CGPoint, _ button: CGMouseButton, _ clicks: Int) {
        guard let event = CGEvent(mouseEventSource: CGEventSource(stateID: .combinedSessionState), mouseType: type, mouseCursorPosition: point, mouseButton: button) else { return }
        if clicks > 1 { event.setIntegerValueField(.mouseEventClickState, value: Int64(clicks)) }
        event.post(tap: .cghidEventTap)
    }
}
