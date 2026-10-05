import AppKit
import CoreGraphics

// Whether the person's screen is locked. While it is, macOS exposes no
// application window through accessibility — every application answers with its
// own element in place of its windows — so nothing can be read or clicked by
// ref or point, however healthy the application is. Typing, keys and screen
// captures still go through.
enum Screen {
    static func locked() -> Bool {
        guard let session = CGSessionCopyCurrentDictionary() as? [String: Any] else { return false }
        return (session["CGSSessionScreenIsLocked"] as? Bool) ?? false
    }
}
