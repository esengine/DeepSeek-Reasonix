import Darwin
import Foundation

enum Processes {
    // Other processes running this same executable. Measured on macOS 15: once
    // one of them has captured a window, a capture in any other never returns,
    // for any application and with no expiry, until that process exits. It is
    // what a capture that ran out of time has to say for itself.
    static func siblings() -> [pid_t] {
        let me = getpid()
        guard let mine = path(of: me) else { return [] }
        var pids = [pid_t](repeating: 0, count: 8192)
        let bytes = proc_listpids(UInt32(PROC_ALL_PIDS), 0, &pids, Int32(pids.count * MemoryLayout<pid_t>.size))
        guard bytes > 0 else { return [] }
        return pids[0..<(Int(bytes) / MemoryLayout<pid_t>.size)]
            .filter { $0 > 0 && $0 != me && path(of: $0) == mine }
    }

    private static func path(of pid: pid_t) -> String? {
        // PROC_PIDPATHINFO_MAXSIZE, which libproc.h does not export to Swift.
        var buffer = [CChar](repeating: 0, count: 4 * Int(MAXPATHLEN))
        guard proc_pidpath(pid, &buffer, UInt32(buffer.count)) > 0 else { return nil }
        return String(cString: buffer)
    }
}
