import AppKit
import ScreenCaptureKit

enum Capture {
    // window captures an application's frontmost on-screen window by itself,
    // whatever covers it, at the display's pixel scale. bounds is where it sits
    // in global points, which is what a click at a point in the image maps to.
    //
    // The answer arrives on whichever thread ScreenCaptureKit finishes on: its
    // continuations can need the main thread, so waiting for one there is a
    // deadlock that reads as a capture that never finished.
    static func window(pid: pid_t, answer: @escaping (Result<JSON, Failure>) -> Void) throws {
        guard #available(macOS 14.0, *) else {
            throw Failure(code: "computer.unsupported", message: "capturing one window needs macOS 14 or later")
        }
        try capture(pid: pid, answer: answer)
    }

    @available(macOS 14.0, *)
    private static func capture(pid: pid_t, answer: @escaping (Result<JSON, Failure>) -> Void) throws {
        try Permissions.requireScreenRecording()
        let once = Once(answer)
        Task.detached {
            do {
                let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
                guard let target = content.windows
                    .filter({ $0.owningApplication?.processID == pid && $0.windowLayer == 0 && $0.frame.width > 1 })
                    .first else {
                    once.fire(.failure(Failure(code: "computer.no_window", message: "the application has no window on screen")))
                    return
                }
                let filter = SCContentFilter(desktopIndependentWindow: target)
                let config = SCStreamConfiguration()
                config.width = Int(target.frame.width * CGFloat(filter.pointPixelScale))
                config.height = Int(target.frame.height * CGFloat(filter.pointPixelScale))
                config.showsCursor = false
                let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
                let rep = NSBitmapImageRep(cgImage: image)
                guard let jpeg = rep.representation(using: .jpeg, properties: [.compressionFactor: 0.8]) else {
                    once.fire(.failure(Failure(code: "computer.capture_failed", message: "the capture could not be encoded")))
                    return
                }
                once.fire(.success([
                    "data": jpeg.base64EncodedString(),
                    "mime": "image/jpeg",
                    "width": image.width,
                    "height": image.height,
                    "window": target.windowID,
                    "bounds": ["x": target.frame.minX, "y": target.frame.minY, "width": target.frame.width, "height": target.frame.height],
                ]))
            } catch {
                once.fire(.failure(Failure(code: "computer.capture_failed", message: "\(error.localizedDescription)")))
            }
        }
        DispatchQueue.global().asyncAfter(deadline: .now() + 10) {
            var said = "the capture did not finish within 10s"
            let others = Processes.siblings()
            if !others.isEmpty {
                said += "; another helper is running (pid \(others.map(String.init).joined(separator: ", "))) and while one of them has captured, the other's capture never returns — quit the other Studio"
            }
            once.fire(.failure(Failure(code: "computer.capture_failed", message: said)))
        }
    }
}

// Once carries a request's single answer: the capture and the deadline race for
// it, and the request is owed exactly one reply.
private final class Once {
    private let answer: (Result<JSON, Failure>) -> Void
    private let lock = NSLock()
    private var fired = false

    init(_ answer: @escaping (Result<JSON, Failure>) -> Void) { self.answer = answer }

    func fire(_ outcome: Result<JSON, Failure>) {
        lock.lock()
        let first = !fired
        fired = true
        lock.unlock()
        if first { answer(outcome) }
    }
}
