import AppKit
import CoreGraphics

enum Apps {
    // The applications a person can see, with their windows on screen. Window
    // names need screen recording; without it a window is listed untitled.
    static func list() -> [JSON] {
        let windows = (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [JSON]) ?? []
        var byPid: [pid_t: [JSON]] = [:]
        for w in windows {
            guard (w[kCGWindowLayer as String] as? Int) == 0,
                  let pid = w[kCGWindowOwnerPID as String] as? pid_t,
                  let id = w[kCGWindowNumber as String] as? Int,
                  let boundsDict = w[kCGWindowBounds as String] as? NSDictionary,
                  let bounds = CGRect(dictionaryRepresentation: boundsDict) else { continue }
            byPid[pid, default: []].append([
                "id": id,
                "title": w[kCGWindowName as String] as? String ?? "",
                "bounds": ["x": bounds.minX, "y": bounds.minY, "width": bounds.width, "height": bounds.height],
            ])
        }
        return NSWorkspace.shared.runningApplications
            .filter { $0.activationPolicy == .regular }
            .map { app in
                [
                    "pid": app.processIdentifier,
                    "bundle": app.bundleIdentifier ?? "",
                    "name": app.localizedName ?? "",
                    "active": app.isActive,
                    "windows": byPid[app.processIdentifier] ?? [],
                ]
            }
    }
}
