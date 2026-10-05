#include "helper.h"

#include <objidl.h>
#include <gdiplus.h>

#include <algorithm>
#include <atomic>
#include <chrono>
#include <thread>

// The agent's own pointer: drawn over everything, taking no clicks and no
// focus, so a person can see where it acts without it touching theirs. It lives
// on a thread of its own because the window and the Escape hook both need a
// message loop, and the request loop is busy doing the work it shows.

namespace {

constexpr UINT moveMessage = WM_APP + 1;
constexpr UINT hideMessage = WM_APP + 2;
constexpr UINT stopMessage = WM_APP + 3;
constexpr UINT_PTR glideTimer = 1, hideTimer = 2;
constexpr int glideMs = 250;

HWND overlay = nullptr;
HHOOK escHook = nullptr;
std::atomic<bool> showing{false};
float scale = 1;
int width = 120, height = 44;
POINT at{}, from{}, to{};
ULONGLONG glideStart = 0;

void render() {
    BITMAPINFO bi{};
    bi.bmiHeader.biSize = sizeof bi.bmiHeader;
    bi.bmiHeader.biWidth = width;
    bi.bmiHeader.biHeight = -height;
    bi.bmiHeader.biPlanes = 1;
    bi.bmiHeader.biBitCount = 32;
    bi.bmiHeader.biCompression = BI_RGB;
    void* bits = nullptr;
    HDC screen = GetDC(nullptr);
    HDC mem = CreateCompatibleDC(screen);
    HBITMAP dib = CreateDIBSection(mem, &bi, DIB_RGB_COLORS, &bits, nullptr, 0);
    HGDIOBJ old = SelectObject(mem, dib);
    {
        Gdiplus::Bitmap canvas(width, height, width * 4, PixelFormat32bppPARGB, static_cast<BYTE*>(bits));
        Gdiplus::Graphics g(&canvas);
        g.SetSmoothingMode(Gdiplus::SmoothingModeAntiAlias);
        g.SetTextRenderingHint(Gdiplus::TextRenderingHintAntiAliasGridFit);
        g.ScaleTransform(scale, scale);
        Gdiplus::Color indigo(255, 88, 86, 214);
        Gdiplus::PointF arrow[] = {{2, 2}, {2, 22}, {7.5f, 17}, {11, 25}, {14, 23.5f}, {10.5f, 16}, {17, 16}};
        Gdiplus::SolidBrush fill(indigo);
        g.FillPolygon(&fill, arrow, 7);
        Gdiplus::Pen outline(Gdiplus::Color(255, 255, 255, 255), 1.5f);
        g.DrawPolygon(&outline, arrow, 7);

        Gdiplus::FontFamily family(L"Segoe UI");
        Gdiplus::Font font(&family, 11, Gdiplus::FontStyleBold, Gdiplus::UnitPixel);
        Gdiplus::RectF size;
        g.MeasureString(L"Reasonix", -1, &font, Gdiplus::PointF(0, 0), &size);
        Gdiplus::RectF pill(16, 23, size.Width + 12, size.Height + 2);
        Gdiplus::GraphicsPath round;
        float r = pill.Height / 2;
        round.AddArc(pill.X, pill.Y, r * 2, pill.Height, 90, 180);
        round.AddArc(pill.X + pill.Width - r * 2, pill.Y, r * 2, pill.Height, 270, 180);
        round.CloseFigure();
        g.FillPath(&fill, &round);
        Gdiplus::SolidBrush white(Gdiplus::Color(255, 255, 255, 255));
        g.DrawString(L"Reasonix", -1, &font, Gdiplus::PointF(pill.X + 6, pill.Y + 1), &white);
    }
    POINT origin{at.x - static_cast<LONG>(2 * scale), at.y - static_cast<LONG>(2 * scale)};
    SIZE extent{width, height};
    POINT zero{0, 0};
    BLENDFUNCTION blend{AC_SRC_OVER, 0, 255, AC_SRC_ALPHA};
    UpdateLayeredWindow(overlay, screen, &origin, &extent, mem, &zero, 0, &blend, ULW_ALPHA);
    SelectObject(mem, old);
    DeleteObject(dib);
    DeleteDC(mem);
    ReleaseDC(nullptr, screen);
}

void hide() {
    KillTimer(overlay, glideTimer);
    KillTimer(overlay, hideTimer);
    ShowWindow(overlay, SW_HIDE);
    showing = false;
    if (escHook) {
        UnhookWindowsHookEx(escHook);
        escHook = nullptr;
    }
}

// Escape while the agent's cursor is on screen is the person saying stop. Any
// other time it is their own keystroke, and one this helper injected never is.
LRESULT CALLBACK onKey(int code, WPARAM wParam, LPARAM lParam) {
    if (code == HC_ACTION && (wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN)) {
        auto* k = reinterpret_cast<KBDLLHOOKSTRUCT*>(lParam);
        // Answered off the hook: a hook that waits on stdout behind a large
        // reply overruns its timeout, and Windows removes it for good.
        if (k->vkCode == VK_ESCAPE && !(k->flags & LLKHF_INJECTED) && showing) PostMessageW(overlay, stopMessage, 0, 0);
    }
    return CallNextHookEx(nullptr, code, wParam, lParam);
}

LRESULT CALLBACK proc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam) {
    switch (msg) {
    case moveMessage:
        to = POINT{static_cast<LONG>(static_cast<int>(wParam)), static_cast<LONG>(static_cast<int>(lParam))};
        if (!showing) {
            at = from = to;
            render();
            ShowWindow(hwnd, SW_SHOWNOACTIVATE);
            showing = true;
            // Held only while the cursor shows: a system-wide hook costs every
            // keystroke on the machine a hop through this thread.
            if (!escHook) escHook = SetWindowsHookExW(WH_KEYBOARD_LL, onKey, GetModuleHandleW(nullptr), 0);
        } else {
            from = at;
            glideStart = GetTickCount64();
            SetTimer(hwnd, glideTimer, 15, nullptr);
        }
        SetTimer(hwnd, hideTimer, 4000, nullptr);
        return 0;
    case hideMessage:
        hide();
        return 0;
    case stopMessage:
        hide();
        reply(Json::object().set("event", "stop"));
        return 0;
    case WM_TIMER:
        if (wParam == hideTimer) {
            hide();
        } else if (wParam == glideTimer) {
            double t = std::min(1.0, static_cast<double>(GetTickCount64() - glideStart) / glideMs);
            double ease = t < 0.5 ? 2 * t * t : 1 - (2 - 2 * t) * (2 - 2 * t) / 2;
            at = POINT{static_cast<LONG>(from.x + (to.x - from.x) * ease), static_cast<LONG>(from.y + (to.y - from.y) * ease)};
            render();
            if (t >= 1) KillTimer(hwnd, glideTimer);
        }
        return 0;
    }
    return DefWindowProcW(hwnd, msg, wParam, lParam);
}

void run(HANDLE ready) {
    Gdiplus::GdiplusStartupInput input;
    ULONG_PTR token = 0;
    Gdiplus::GdiplusStartup(&token, &input, nullptr);
    scale = GetDpiForSystem() / 96.0f;
    width = static_cast<int>(120 * scale);
    height = static_cast<int>(44 * scale);
    WNDCLASSW wc{};
    wc.lpfnWndProc = proc;
    wc.hInstance = GetModuleHandleW(nullptr);
    wc.lpszClassName = L"ReasonixAgentCursor";
    RegisterClassW(&wc);
    overlay = CreateWindowExW(WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE,
                              wc.lpszClassName, L"", WS_POPUP, 0, 0, width, height, nullptr, nullptr, wc.hInstance, nullptr);
    SetEvent(ready);
    MSG msg;
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) DispatchMessageW(&msg);
}

} // namespace

void cursorStart() {
    HANDLE ready = CreateEventW(nullptr, TRUE, FALSE, nullptr);
    std::thread(run, ready).detach();
    WaitForSingleObject(ready, 5000);
    CloseHandle(ready);
}

// cursorMove glides the agent's cursor to a point on screen and waits out the
// glide, so what the person sees lands before the action it shows.
void cursorMove(POINT point) {
    if (!overlay) return;
    PostMessageW(overlay, moveMessage, static_cast<WPARAM>(point.x), static_cast<LPARAM>(point.y));
    std::this_thread::sleep_for(std::chrono::milliseconds(glideMs + 30));
}

void cursorHide() {
    if (overlay) PostMessageW(overlay, hideMessage, 0, 0);
}
