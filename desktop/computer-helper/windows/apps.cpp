#include "helper.h"

#include <map>

// listApps answers the applications a person can see, each with its windows on
// screen in physical pixels. A minimized application is listed with none.
Json listApps() {
    struct Walk { std::vector<DWORD> order; std::map<DWORD, bool> listed; } w;
    EnumWindows([](HWND hwnd, LPARAM l) -> BOOL {
        auto* w = reinterpret_cast<Walk*>(l);
        if (!appListed(hwnd)) return TRUE;
        DWORD pid = contentPid(hwnd);
        if (pid == 0 || pid == GetCurrentProcessId() || w->listed.count(pid)) return TRUE;
        w->listed[pid] = true;
        w->order.push_back(pid);
        return TRUE;
    }, reinterpret_cast<LPARAM>(&w));

    HWND fg = GetForegroundWindow();
    DWORD active = fg ? contentPid(fg) : 0;
    Json apps = Json::array();
    for (DWORD pid : w.order) {
        Json windows = Json::array();
        for (HWND hwnd : appWindows(pid, false)) {
            wchar_t title[512];
            int n = GetWindowTextW(hwnd, title, ARRAYSIZE(title));
            windows.push(Json::object()
                             .set("id", static_cast<long long>(reinterpret_cast<uintptr_t>(hwnd)))
                             .set("title", narrow(std::wstring(title, n > 0 ? n : 0)))
                             .set("bounds", rectJson(windowBounds(hwnd))));
        }
        apps.push(Json::object()
                      .set("pid", pid)
                      .set("bundle", identity(pid))
                      .set("exe", exeName(pid))
                      .set("name", displayName(pid))
                      .set("active", pid == active)
                      .set("windows", windows));
    }
    return apps;
}
