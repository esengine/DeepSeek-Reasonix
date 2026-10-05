import AppKit
import Foundation

// The computer-use helper: the one process in Studio that reads other
// applications and puts input into them. It decides nothing. The kernel chooses
// every target and step after its own permission check, and this carries each
// out, answering one JSON line per request line on stdin.

struct Failure: Error {
    let code: String
    let message: String
}

typealias JSON = [String: Any]

let refs = RefTable()
let cursor = VirtualCursor()

func pointParam(_ params: JSON) throws -> CGPoint {
    CGPoint(x: try numberParam(params, "x"), y: try numberParam(params, "y"))
}

func handle(_ method: String, _ params: JSON) throws -> JSON {
    switch method {
    case "status":
        return Permissions.status()
    case "request_permission":
        return try Permissions.request(params["which"] as? String ?? "")
    case "apps":
        return ["apps": Apps.list()]
    case "snapshot":
        return try Accessibility.snapshot(pid: try pidParam(params), refs: refs)
    case "press":
        return try Accessibility.press(pid: try pidParam(params), ref: try stringParam(params, "ref"), refs: refs, cursor: cursor)
    case "click":
        return try Accessibility.click(pid: try pidParam(params), x: try numberParam(params, "x"), y: try numberParam(params, "y"), cursor: cursor)
    case "menu":
        return try Accessibility.menu(pid: try pidParam(params), ref: try stringParam(params, "ref"), refs: refs, cursor: cursor)
    case "scroll":
        return try Accessibility.scroll(pid: try pidParam(params), ref: (params["ref"] as? String) ?? "", amount: (params["amount"] as? Double) ?? 0, refs: refs, cursor: cursor)
    case "focus":
        return try Accessibility.focus(pid: try pidParam(params), ref: try stringParam(params, "ref"), refs: refs, cursor: cursor)
    case "set_value":
        return try Accessibility.setValue(pid: try pidParam(params), ref: try stringParam(params, "ref"), text: try stringParam(params, "text"), refs: refs, cursor: cursor)
    case "type":
        return try Keyboard.type(pid: try pidParam(params), text: try stringParam(params, "text"))
    case "key":
        return try Keyboard.press(pid: try pidParam(params), chord: try stringParam(params, "key"), times: (params["times"] as? Int) ?? 1)
    case "paste":
        return try Clipboard.paste(pid: try pidParam(params), text: try stringParam(params, "text"))
    case "pointer_move":
        return try Pointer.move(pid: try pidParam(params), to: try pointParam(params), cursor: cursor)
    case "pointer_click":
        return try Pointer.click(pid: try pidParam(params), at: try pointParam(params), button: (params["button"] as? String) ?? "left", clicks: (params["clicks"] as? Int) ?? 1, cursor: cursor)
    case "pointer_drag":
        return try Pointer.drag(pid: try pidParam(params), from: try pointParam(params), to: CGPoint(x: try numberParam(params, "to_x"), y: try numberParam(params, "to_y")), cursor: cursor)
    case "pointer_position":
        return Pointer.position()
    case "pointer_release":
        cursor.hide()
        return Pointer.release()
    case "hold_key":
        return try Keyboard.hold(pid: try pidParam(params), chord: try stringParam(params, "key"), seconds: (params["seconds"] as? Double) ?? 1)
    case "cursor_hide":
        cursor.hide()
        return [:]
    default:
        throw Failure(code: "computer.bad_request", message: "unknown method \(method)")
    }
}

func pidParam(_ params: JSON) throws -> pid_t {
    guard let n = params["pid"] as? NSNumber, n.int32Value > 0 else {
        throw Failure(code: "computer.bad_request", message: "pid is required")
    }
    return n.int32Value
}

func stringParam(_ params: JSON, _ key: String) throws -> String {
    guard let s = params[key] as? String else {
        throw Failure(code: "computer.bad_request", message: "\(key) is required")
    }
    return s
}

func numberParam(_ params: JSON, _ key: String) throws -> Double {
    guard let n = params[key] as? NSNumber else {
        throw Failure(code: "computer.bad_request", message: "\(key) is required")
    }
    return n.doubleValue
}

// A capture answers from whichever thread it finished on, so the one stream
// every answer goes down is written under a lock of its own.
let stdoutLock = NSLock()

func reply(_ body: JSON) {
    guard let data = try? JSONSerialization.data(withJSONObject: body), var line = String(data: data, encoding: .utf8) else { return }
    line += "\n"
    stdoutLock.lock()
    FileHandle.standardOutput.write(line.data(using: .utf8)!)
    stdoutLock.unlock()
}

func fail(_ id: Any, _ error: Error) {
    if let failure = error as? Failure {
        reply(["id": id, "error": ["code": failure.code, "message": failure.message]])
    } else {
        reply(["id": id, "error": ["code": "computer.failed", "message": "\(error)"]])
    }
}

// Requests are read off the main thread and answered on it: the overlay and the
// accessibility calls it animates alongside both belong to the main run loop.
Thread.detachNewThread {
    while let line = readLine() {
        guard let data = line.data(using: .utf8),
              let request = (try? JSONSerialization.jsonObject(with: data)) as? JSON,
              let id = request["id"] else { continue }
        let method = request["method"] as? String ?? ""
        let params = request["params"] as? JSON ?? [:]
        DispatchQueue.main.async {
            do {
                // A capture is the one operation this process waits on rather
                // than performs, and the main thread is what the rest of them
                // need: it answers when it is done, out of this order.
                if method == "screenshot" {
                    try Capture.window(pid: try pidParam(params)) { outcome in
                        switch outcome {
                        case .success(let body): reply(["id": id, "result": body])
                        case .failure(let failure): fail(id, failure)
                        }
                    }
                    return
                }
                reply(["id": id, "result": try handle(method, params)])
            } catch {
                fail(id, error)
            }
        }
    }
    // Behind every request already queued: stdin closing is the kernel letting
    // go, and what it asked before that is still owed an answer.
    DispatchQueue.main.async { exit(0) }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.run()
