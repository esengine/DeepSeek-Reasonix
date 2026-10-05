// The computer-use helper for Windows: the one process in Studio that reads
// other applications and puts input into them. It decides nothing. The kernel
// chooses every target and step after its own permission check, and this
// carries each out, answering one JSON line per request line on stdin.

#include "helper.h"

#include <fcntl.h>
#include <io.h>
#include <objbase.h>

#include <algorithm>
#include <cctype>
#include <cmath>
#include <cstdio>
#include <iostream>
#include <mutex>
#include <set>

std::string narrow(const std::wstring& w) {
    if (w.empty()) return {};
    int n = WideCharToMultiByte(CP_UTF8, 0, w.data(), static_cast<int>(w.size()), nullptr, 0, nullptr, nullptr);
    std::string out(n, '\0');
    WideCharToMultiByte(CP_UTF8, 0, w.data(), static_cast<int>(w.size()), out.data(), n, nullptr, nullptr);
    return out;
}

std::wstring widen(const std::string& s) {
    if (s.empty()) return {};
    int n = MultiByteToWideChar(CP_UTF8, 0, s.data(), static_cast<int>(s.size()), nullptr, 0);
    std::wstring out(n, L'\0');
    MultiByteToWideChar(CP_UTF8, 0, s.data(), static_cast<int>(s.size()), out.data(), n);
    return out;
}

std::string lower(std::string s) {
    std::transform(s.begin(), s.end(), s.begin(), [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
    return s;
}

std::string num(double v) {
    char buf[32];
    std::snprintf(buf, sizeof buf, "%.0f", v);
    return buf;
}

DWORD pidParam(const Json& params) {
    const Json* v = params.get("pid");
    if (!v || !v->isNumber() || v->number() <= 0 || v->number() > 0xFFFFFFFF) {
        throw Failure{"computer.bad_request", "pid is required"};
    }
    return static_cast<DWORD>(v->number());
}

std::string stringParam(const Json& params, const char* key) {
    const Json* v = params.get(key);
    if (!v || !v->isString()) throw Failure{"computer.bad_request", std::string(key) + " is required"};
    return v->string();
}

double numberParam(const Json& params, const char* key) {
    const Json* v = params.get(key);
    if (!v || !v->isNumber()) throw Failure{"computer.bad_request", std::string(key) + " is required"};
    return v->number();
}

double numberOr(const Json& params, const char* key, double fallback) {
    const Json* v = params.get(key);
    return v && v->isNumber() ? v->number() : fallback;
}

static POINT pointParam(const Json& params, const char* x, const char* y) {
    return POINT{static_cast<LONG>(std::lround(numberParam(params, x))), static_cast<LONG>(std::lround(numberParam(params, y)))};
}

// The kernel's stop event and a request's answer can come from different
// threads, so the one stream every line goes down is written under a lock.
static std::mutex stdoutLock;

void reply(const Json& body) {
    std::string line = body.dump() + "\n";
    std::lock_guard<std::mutex> hold(stdoutLock);
    std::fwrite(line.data(), 1, line.size(), stdout);
    std::fflush(stdout);
}

static Json handle(const std::string& method, const Json& params) {
    if (method == "status") return status();
    if (method == "request_permission") {
        std::string which = params.get("which") && params.get("which")->isString() ? params.get("which")->string() : "";
        if (which != "accessibility" && which != "screen_recording") {
            throw Failure{"computer.bad_request", "which must be accessibility or screen_recording"};
        }
        return status();
    }
    if (method == "apps") return Json::object().set("apps", listApps());
    if (method == "pointer_position") return pointerPosition();
    if (method == "pointer_release") {
        cursorHide();
        return pointerRelease();
    }
    if (method == "cursor_hide") {
        cursorHide();
        return Json::object();
    }

    static const std::set<std::string> perApp = {"snapshot", "screenshot", "press", "click", "menu", "scroll", "focus", "set_value",
                                                 "type", "key", "hold_key", "paste", "pointer_move", "pointer_click", "pointer_drag"};
    if (!perApp.count(method)) throw Failure{"computer.bad_request", "unknown method " + method};
    DWORD pid = pidParam(params);
    requireOperable(pid);
    if (method == "snapshot") return snapshot(pid);
    if (method == "screenshot") return capture(pid);
    if (method == "press") return press(pid, stringParam(params, "ref"));
    if (method == "click") return clickAt(pid, numberParam(params, "x"), numberParam(params, "y"));
    if (method == "menu") return menu(pid, stringParam(params, "ref"));
    if (method == "scroll") {
        std::string ref = params.get("ref") && params.get("ref")->isString() ? params.get("ref")->string() : "";
        return scroll(pid, ref, numberOr(params, "amount", 0));
    }
    if (method == "focus") return focus(pid, stringParam(params, "ref"));
    if (method == "set_value") return setValue(pid, stringParam(params, "ref"), stringParam(params, "text"));
    if (method == "type") return typeText(pid, stringParam(params, "text"));
    if (method == "key") return pressKey(pid, stringParam(params, "key"), static_cast<int>(numberOr(params, "times", 1)));
    if (method == "hold_key") return holdKey(pid, stringParam(params, "key"), numberOr(params, "seconds", 1));
    if (method == "paste") return paste(pid, stringParam(params, "text"));
    if (method == "pointer_move") return pointerMove(pid, pointParam(params, "x", "y"));
    if (method == "pointer_click") {
        std::string button = params.get("button") && params.get("button")->isString() ? params.get("button")->string() : "left";
        return pointerClick(pid, pointParam(params, "x", "y"), button, static_cast<int>(numberOr(params, "clicks", 1)));
    }
    return pointerDrag(pid, pointParam(params, "x", "y"), pointParam(params, "to_x", "to_y"));
}

int main() {
    // Every coordinate this helper reads or writes is a physical pixel: window
    // bounds, accessibility rectangles, captures and injected input all agree.
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    _setmode(_fileno(stdin), _O_BINARY);
    _setmode(_fileno(stdout), _O_BINARY);
    if (FAILED(CoInitializeEx(nullptr, COINIT_MULTITHREADED))) return 1;
    uiaStart();
    cursorStart();

    std::string line;
    while (std::getline(std::cin, line)) {
        if (!line.empty() && line.back() == '\r') line.pop_back();
        Json request;
        if (!Json::parse(line, request) || !request.isObject()) continue;
        const Json* id = request.get("id");
        if (!id) continue;
        const Json* m = request.get("method");
        std::string method = m && m->isString() ? m->string() : "";
        const Json* p = request.get("params");
        Json params = p && p->isObject() ? *p : Json::object();
        try {
            reply(Json::object().set("id", *id).set("result", handle(method, params)));
        } catch (const Failure& f) {
            reply(Json::object().set("id", *id).set("error", Json::object().set("code", f.code).set("message", f.message)));
        } catch (const std::exception& e) {
            reply(Json::object().set("id", *id).set("error", Json::object().set("code", "computer.failed").set("message", e.what())));
        }
    }
    // stdin closing is the kernel letting go; a clipboard still borrowed goes
    // back to the person before this process does.
    clipboardSettle();
    return 0;
}
