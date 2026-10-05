#include "helper.h"

#include <appmodel.h>
#include <dwmapi.h>

#include <chrono>
#include <thread>

namespace {

struct Handle {
    HANDLE h;
    explicit Handle(HANDLE h) : h(h) {}
    ~Handle() { if (h) CloseHandle(h); }
    Handle(const Handle&) = delete;
    Handle& operator=(const Handle&) = delete;
};

std::wstring imagePath(DWORD pid) {
    Handle p(OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid));
    if (!p.h) return {};
    wchar_t buf[MAX_PATH * 4];
    DWORD n = ARRAYSIZE(buf);
    return QueryFullProcessImageNameW(p.h, 0, buf, &n) ? std::wstring(buf, n) : std::wstring();
}

std::wstring fileName(const std::wstring& path) {
    size_t slash = path.find_last_of(L"\\/");
    return slash == std::wstring::npos ? path : path.substr(slash + 1);
}

std::wstring packageFamily(DWORD pid) {
    Handle p(OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid));
    if (!p.h) return {};
    UINT32 len = 0;
    if (GetPackageFamilyName(p.h, &len, nullptr) != ERROR_INSUFFICIENT_BUFFER || len == 0) return {};
    std::wstring name(len, L'\0');
    if (GetPackageFamilyName(p.h, &len, name.data()) != ERROR_SUCCESS) return {};
    name.resize(wcslen(name.c_str()));
    return name;
}

bool cloaked(HWND hwnd) {
    DWORD c = 0;
    return SUCCEEDED(DwmGetWindowAttribute(hwnd, DWMWA_CLOAKED, &c, sizeof c)) && c != 0;
}

std::wstring className(HWND hwnd) {
    wchar_t buf[256];
    int n = GetClassNameW(hwnd, buf, ARRAYSIZE(buf));
    return std::wstring(buf, n > 0 ? n : 0);
}

// The shell's own surfaces are windows of explorer.exe like any other, and
// none of them is an application a person would name.
bool shellSurface(HWND hwnd) {
    std::wstring c = className(hwnd);
    return c == L"Progman" || c == L"WorkerW" || c == L"Shell_TrayWnd" || c == L"Shell_SecondaryTrayWnd" || c == L"#32768";
}

// A console window reports whichever program owns the console — cmd, python,
// wsl, ssh — rather than its host, so no name can refuse it. It is recognised
// by what it is and belongs to no application this helper will operate.
bool console(HWND hwnd) {
    std::wstring c = className(hwnd);
    return c == L"ConsoleWindowClass" || c == L"PseudoConsoleWindow" || c == L"CASCADIA_HOSTING_WINDOW_CLASS";
}

bool intact(HWND hwnd) {
    if (!IsWindowVisible(hwnd) || cloaked(hwnd) || shellSurface(hwnd) || console(hwnd)) return false;
    return (GetWindowLongPtrW(hwnd, GWL_EXSTYLE) & WS_EX_TOOLWINDOW) == 0;
}

enum class Level { Known, Denied, Unknown };

DWORD integrity(HANDLE process, Level& known) {
    known = Level::Unknown;
    HANDLE token = nullptr;
    if (!OpenProcessToken(process, TOKEN_QUERY, &token)) {
        if (GetLastError() == ERROR_ACCESS_DENIED) known = Level::Denied;
        return 0;
    }
    Handle t(token);
    DWORD size = 0;
    GetTokenInformation(token, TokenIntegrityLevel, nullptr, 0, &size);
    std::vector<BYTE> buf(size);
    if (size == 0 || !GetTokenInformation(token, TokenIntegrityLevel, buf.data(), size, &size)) return 0;
    auto* label = reinterpret_cast<TOKEN_MANDATORY_LABEL*>(buf.data());
    known = Level::Known;
    return *GetSidSubAuthority(label->Label.Sid, *GetSidSubAuthorityCount(label->Label.Sid) - 1);
}

} // namespace

std::string identity(DWORD pid) {
    std::wstring family = packageFamily(pid);
    if (!family.empty()) return narrow(family);
    return exeName(pid);
}

std::string exeName(DWORD pid) { return lower(narrow(fileName(imagePath(pid)))); }

std::string displayName(DWORD pid) {
    std::wstring path = imagePath(pid);
    DWORD ignored = 0;
    DWORD size = GetFileVersionInfoSizeW(path.c_str(), &ignored);
    if (size > 0) {
        std::vector<BYTE> info(size);
        struct Translation { WORD language, codepage; }* tr = nullptr;
        UINT len = 0;
        if (GetFileVersionInfoW(path.c_str(), 0, size, info.data()) &&
            VerQueryValueW(info.data(), L"\\VarFileInfo\\Translation", reinterpret_cast<void**>(&tr), &len) && len >= sizeof *tr) {
            wchar_t key[64];
            swprintf_s(key, L"\\StringFileInfo\\%04x%04x\\FileDescription", tr->language, tr->codepage);
            wchar_t* desc = nullptr;
            if (VerQueryValueW(info.data(), key, reinterpret_cast<void**>(&desc), &len) && len > 1 && desc[0]) {
                return narrow(desc);
            }
        }
    }
    std::wstring name = fileName(path);
    size_t dot = name.find_last_of(L'.');
    return narrow(dot == std::wstring::npos ? name : name.substr(0, dot));
}

// contentPid is the process a top-level window shows. A Store application's
// frame belongs to ApplicationFrameHost; what is inside it is the application's
// own CoreWindow, and a frame with none inside has nothing of it to show.
DWORD contentPid(HWND top) {
    DWORD pid = 0;
    if (console(top)) return 0;
    GetWindowThreadProcessId(top, &pid);
    if (className(top) != L"ApplicationFrameWindow") return pid;
    struct Find { DWORD host; DWORD found; } f{pid, 0};
    EnumChildWindows(top, [](HWND child, LPARAM l) -> BOOL {
        auto* f = reinterpret_cast<Find*>(l);
        if (className(child) != L"Windows.UI.Core.CoreWindow") return TRUE;
        DWORD p = 0;
        GetWindowThreadProcessId(child, &p);
        if (p == f->host) return TRUE;
        f->found = p;
        return FALSE;
    }, reinterpret_cast<LPARAM>(&f));
    return f.found;
}

// appListed is whether a window makes its process an application a person
// can see: the windows the task switcher offers, minimized ones included.
bool appListed(HWND top) {
    if (!intact(top)) return false;
    LONG_PTR ex = GetWindowLongPtrW(top, GWL_EXSTYLE);
    if (ex & WS_EX_APPWINDOW) return true;
    return GetWindow(top, GW_OWNER) == nullptr && (ex & WS_EX_NOACTIVATE) == 0;
}

// appWindows lists a process's top-level windows front to back, dialogs it
// owns included, with the one in front of the person first.
std::vector<HWND> appWindows(DWORD pid, bool includeMinimized) {
    struct Walk { DWORD pid; bool minimized; std::vector<HWND> out; } w{pid, includeMinimized, {}};
    EnumWindows([](HWND hwnd, LPARAM l) -> BOOL {
        auto* w = reinterpret_cast<Walk*>(l);
        if (!intact(hwnd) || contentPid(hwnd) != w->pid) return TRUE;
        if (IsIconic(hwnd)) {
            if (w->minimized) w->out.push_back(hwnd);
            return TRUE;
        }
        RECT r = windowBounds(hwnd);
        if (r.right - r.left > 1 && r.bottom - r.top > 1) w->out.push_back(hwnd);
        return TRUE;
    }, reinterpret_cast<LPARAM>(&w));
    HWND fg = GetForegroundWindow();
    for (size_t i = 1; i < w.out.size(); i++) {
        if (w.out[i] == fg) {
            w.out.erase(w.out.begin() + i);
            w.out.insert(w.out.begin(), fg);
            break;
        }
    }
    return w.out;
}

// requireOperable refuses a process that is gone, and one Windows keeps this
// helper from reaching: input and accessibility stop at a process running with
// more privilege, and failing there without a cause reads as a broken app.
void requireOperable(DWORD pid) {
    Handle p(OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid));
    bool unreachable = !p.h && GetLastError() == ERROR_ACCESS_DENIED;
    DWORD code = 0;
    if (!unreachable && (!p.h || !GetExitCodeProcess(p.h, &code) || code != STILL_ACTIVE)) {
        throw Failure{"computer.no_app", "no application with pid " + std::to_string(pid) + " is running"};
    }
    Level known = Level::Unknown, mineKnown = Level::Unknown;
    DWORD theirs = p.h ? integrity(p.h, known) : 0;
    DWORD mine = integrity(GetCurrentProcess(), mineKnown);
    if (unreachable || known == Level::Denied || (known == Level::Known && mineKnown == Level::Known && theirs > mine)) {
        throw Failure{"computer.elevated",
                      "this application runs as administrator and Reasonix Studio does not; Windows keeps a process from reading or "
                      "sending input to one with more privilege. Ask the person to restart it without administrator rights, or to do this step themselves"};
    }
}

bool ownsPoint(DWORD pid, POINT at) {
    HWND hit = WindowFromPoint(at);
    return hit && contentPid(GetAncestor(hit, GA_ROOT)) == pid;
}

bool isFront(DWORD pid) {
    HWND fg = GetForegroundWindow();
    return fg && contentPid(fg) == pid;
}

// front brings the application forward before input that goes wherever the
// foreground is. Windows grants the foreground only to a process that received
// the last input, so an input with no effect is injected first.
void front(DWORD pid) {
    if (isFront(pid)) return;
    std::vector<HWND> wins = appWindows(pid, true);
    if (wins.empty()) throw Failure{"computer.no_window", "the application has no window to bring to the front"};
    HWND target = wins[0];
    if (IsIconic(target)) ShowWindow(target, SW_RESTORE);
    INPUT nudge{};
    nudge.type = INPUT_MOUSE;
    nudge.mi.dwFlags = MOUSEEVENTF_MOVE;
    SendInput(1, &nudge, sizeof nudge);
    SetForegroundWindow(target);
    for (int i = 0; i < 25 && !isFront(pid); i++) std::this_thread::sleep_for(std::chrono::milliseconds(20));
    if (!isFront(pid)) {
        DWORD fgThread = GetWindowThreadProcessId(GetForegroundWindow(), nullptr);
        DWORD me = GetCurrentThreadId();
        if (fgThread && fgThread != me && AttachThreadInput(me, fgThread, TRUE)) {
            BringWindowToTop(target);
            SetForegroundWindow(target);
            AttachThreadInput(me, fgThread, FALSE);
        }
        for (int i = 0; i < 25 && !isFront(pid); i++) std::this_thread::sleep_for(std::chrono::milliseconds(20));
    }
    if (!isFront(pid)) {
        throw Failure{"computer.needs_front",
                      "Windows kept this application from coming to the front; ask the person to click it once, then try again"};
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(150));
}

RECT windowBounds(HWND hwnd) {
    RECT r{};
    if (FAILED(DwmGetWindowAttribute(hwnd, DWMWA_EXTENDED_FRAME_BOUNDS, &r, sizeof r))) GetWindowRect(hwnd, &r);
    return r;
}

Json rectJson(const RECT& r) {
    return Json::object()
        .set("x", static_cast<long>(r.left))
        .set("y", static_cast<long>(r.top))
        .set("width", static_cast<long>(r.right - r.left))
        .set("height", static_cast<long>(r.bottom - r.top));
}
