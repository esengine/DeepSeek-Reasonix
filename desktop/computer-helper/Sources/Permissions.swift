import AppKit
import ApplicationServices
import CoreGraphics

enum Permissions {
    static func status() -> JSON {
        ["accessibility": AXIsProcessTrusted(), "screen_recording": CGPreflightScreenCaptureAccess(), "screen_locked": Screen.locked()]
    }

    // Asks the system to show its own prompt, and opens the pane where the
    // switch lives: a prompt that was dismissed once is not shown again.
    static func request(_ which: String) throws -> JSON {
        let pane: String
        switch which {
        case "accessibility":
            let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
            _ = AXIsProcessTrustedWithOptions(options)
            pane = "Privacy_Accessibility"
        case "screen_recording":
            _ = CGRequestScreenCaptureAccess()
            pane = "Privacy_ScreenCapture"
        default:
            throw Failure(code: "computer.bad_request", message: "which must be accessibility or screen_recording")
        }
        if let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?\(pane)") {
            NSWorkspace.shared.open(url)
        }
        return status()
    }

    static func requireAccessibility() throws {
        if !AXIsProcessTrusted() {
            throw Failure(code: "computer.permission_missing", message: "accessibility")
        }
    }

    static func requireScreenRecording() throws {
        if !CGPreflightScreenCaptureAccess() {
            throw Failure(code: "computer.permission_missing", message: "screen_recording")
        }
    }
}
