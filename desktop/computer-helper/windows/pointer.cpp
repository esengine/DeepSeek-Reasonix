#include "helper.h"

#include <chrono>
#include <cmath>
#include <optional>
#include <thread>

// The person's own pointer, moved and clicked by the agent. Every other path
// here works through accessibility and leaves the pointer alone; this one does
// not, so the kernel asks for it by name and it goes back where it was found.
//
// A point is acted on only while the window under it belongs to the approved
// application: on screen the agent has no window of its own to aim at.

namespace {

std::optional<POINT> parked;
constexpr int dragSteps = 12;

INPUT mouse(POINT at, DWORD flags) {
    double vx = GetSystemMetrics(SM_XVIRTUALSCREEN), vy = GetSystemMetrics(SM_YVIRTUALSCREEN);
    double vw = GetSystemMetrics(SM_CXVIRTUALSCREEN), vh = GetSystemMetrics(SM_CYVIRTUALSCREEN);
    INPUT in{};
    in.type = INPUT_MOUSE;
    in.mi.dx = static_cast<LONG>(std::lround((at.x - vx) * 65535.0 / std::max(vw - 1, 1.0)));
    in.mi.dy = static_cast<LONG>(std::lround((at.y - vy) * 65535.0 / std::max(vh - 1, 1.0)));
    in.mi.dwFlags = flags | MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_VIRTUALDESK;
    return in;
}

void post(POINT at, DWORD flags) {
    INPUT in = mouse(at, flags);
    if (SendInput(1, &in, sizeof in) == 0) {
        throw Failure{"computer.failed", screenLocked() ? "the screen is locked, and Windows delivers no input while it is"
                                                        : "Windows refused the pointer input"};
    }
}

std::string where(POINT at) { return "(" + std::to_string(at.x) + ", " + std::to_string(at.y) + ")"; }

void check(DWORD pid, POINT at) {
    HWND hit = WindowFromPoint(at);
    if (!hit) throw Failure{"computer.no_element", "no window is at " + where(at)};
    if (contentPid(GetAncestor(hit, GA_ROOT)) != pid) {
        throw Failure{"computer.no_element", "the window at " + where(at) + " belongs to another application"};
    }
}

// aimed checks again at the instant of input: between the first check and the
// press a window can come up over the point, and it would take the click.
void aimed(DWORD pid, POINT at) {
    check(pid, at);
    if (!isFront(pid)) throw Failure{"computer.needs_front", "the application left the front before the pointer acted"};
    post(at, 0);
}

// park remembers where the person left their pointer, once per run of steps.
void park() {
    if (parked) return;
    POINT at;
    if (GetCursorPos(&at)) parked = at;
}

void buttons(const std::string& button, DWORD& down, DWORD& up) {
    if (button.empty() || button == "left") {
        down = MOUSEEVENTF_LEFTDOWN, up = MOUSEEVENTF_LEFTUP;
    } else if (button == "right") {
        down = MOUSEEVENTF_RIGHTDOWN, up = MOUSEEVENTF_RIGHTUP;
    } else if (button == "middle") {
        down = MOUSEEVENTF_MIDDLEDOWN, up = MOUSEEVENTF_MIDDLEUP;
    } else {
        throw Failure{"computer.bad_step", button + " is not a mouse button; use left, right or middle"};
    }
}

} // namespace

Json pointerMove(DWORD pid, POINT to) {
    check(pid, to);
    park();
    cursorMove(to);
    post(to, 0);
    return Json::object();
}

// A click into a background window is spent activating it, so taking the
// pointer means taking the foreground with it. What is on top is checked once
// the application is: until then its own window may be covered.
Json pointerClick(DWORD pid, POINT at, const std::string& button, int clicks) {
    DWORD down = 0, up = 0;
    buttons(button, down, up);
    front(pid);
    check(pid, at);
    park();
    cursorMove(at);
    for (int i = 0; i < std::max(clicks, 1); i++) {
        aimed(pid, at);
        post(at, down);
        post(at, up);
    }
    return Json::object();
}

Json pointerDrag(DWORD pid, POINT from, POINT to) {
    front(pid);
    check(pid, from);
    check(pid, to);
    park();
    cursorMove(from);
    aimed(pid, from);
    post(from, MOUSEEVENTF_LEFTDOWN);
    // The button comes back up whatever happens on the way: left down, it is
    // the person's next move that drags.
    try {
        for (int step = 1; step <= dragSteps; step++) {
            double t = static_cast<double>(step) / dragSteps;
            post(POINT{static_cast<LONG>(std::lround(from.x + (to.x - from.x) * t)), static_cast<LONG>(std::lround(from.y + (to.y - from.y) * t))}, 0);
            std::this_thread::sleep_for(std::chrono::milliseconds(20));
        }
        aimed(pid, to);
    } catch (...) {
        INPUT release = mouse(to, MOUSEEVENTF_LEFTUP);
        SendInput(1, &release, sizeof release);
        throw;
    }
    post(to, MOUSEEVENTF_LEFTUP);
    return Json::object();
}

Json pointerPosition() {
    POINT at{};
    GetCursorPos(&at);
    return Json::object().set("x", static_cast<long>(at.x)).set("y", static_cast<long>(at.y));
}

// pointerRelease puts the pointer back where the person left it.
Json pointerRelease() {
    if (!parked) return Json::object();
    POINT home = *parked;
    parked.reset();
    post(home, 0);
    return Json::object().set("returned", true);
}
