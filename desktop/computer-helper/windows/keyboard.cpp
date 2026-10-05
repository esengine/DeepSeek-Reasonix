#include "helper.h"

#include <algorithm>
#include <chrono>
#include <map>
#include <thread>

// Keyboard input on Windows goes wherever the foreground is, so every entry
// point brings the application forward first and checks it is still there
// between chunks: a person switching away must not receive the rest.

namespace {

struct Key {
    WORD vk;
    bool extended;
};

const std::map<std::string, Key> named = [] {
    std::map<std::string, Key> keys = {
        {"enter", {VK_RETURN, false}}, {"return", {VK_RETURN, false}}, {"tab", {VK_TAB, false}},
        {"escape", {VK_ESCAPE, false}}, {"space", {VK_SPACE, false}}, {"backspace", {VK_BACK, false}},
        {"delete", {VK_DELETE, true}}, {"insert", {VK_INSERT, true}}, {"arrowleft", {VK_LEFT, true}},
        {"arrowright", {VK_RIGHT, true}}, {"arrowup", {VK_UP, true}}, {"arrowdown", {VK_DOWN, true}},
        {"home", {VK_HOME, true}}, {"end", {VK_END, true}}, {"pageup", {VK_PRIOR, true}}, {"pagedown", {VK_NEXT, true}},
        {"minus", {VK_OEM_MINUS, false}}, {"equal", {VK_OEM_PLUS, false}}, {"comma", {VK_OEM_COMMA, false}},
        {"period", {VK_OEM_PERIOD, false}}, {"slash", {VK_OEM_2, false}}, {"semicolon", {VK_OEM_1, false}},
        {"quote", {VK_OEM_7, false}}, {"bracketleft", {VK_OEM_4, false}}, {"bracketright", {VK_OEM_6, false}},
        {"backslash", {VK_OEM_5, false}}, {"backquote", {VK_OEM_3, false}},
    };
    for (char c = 'a'; c <= 'z'; c++) keys[std::string(1, c)] = {static_cast<WORD>(c - 'a' + 'A'), false};
    for (char c = '0'; c <= '9'; c++) keys[std::string(1, c)] = {static_cast<WORD>(c), false};
    for (int n = 1; n <= 12; n++) keys["f" + std::to_string(n)] = {static_cast<WORD>(VK_F1 + n - 1), false};
    return keys;
}();

const std::map<std::string, WORD> modifiers = {
    {"shift", WORD{VK_SHIFT}}, {"control", WORD{VK_CONTROL}}, {"ctrl", WORD{VK_CONTROL}}, {"alt", WORD{VK_MENU}}, {"option", WORD{VK_MENU}},
};

struct KeyChord {
    Key key;
    std::vector<WORD> mods;
};

std::string trim(const std::string& s) {
    size_t a = s.find_first_not_of(" \t"), b = s.find_last_not_of(" \t");
    return a == std::string::npos ? "" : s.substr(a, b - a + 1);
}

KeyChord resolve(const std::string& chord) {
    std::vector<std::string> parts;
    size_t at = 0;
    for (;;) {
        size_t plus = chord.find('+', at);
        parts.push_back(lower(trim(chord.substr(at, plus == std::string::npos ? std::string::npos : plus - at))));
        if (plus == std::string::npos) break;
        at = plus + 1;
    }
    auto found = named.find(parts.back());
    if (found == named.end()) {
        throw Failure{"computer.bad_step", chord + " is not a key this can press. It presses named keys — enter, tab, escape, space, backspace, delete, insert, the arrows, home, end, pageup, pagedown, f1 to f12, a letter, a digit, or minus, equal, comma, period, slash, semicolon, quote, bracketleft, bracketright, backslash, backquote — with the "
                                               "modifiers shift, control and alt. Text goes through the type action"};
    }
    KeyChord c{found->second, {}};
    for (size_t i = 0; i + 1 < parts.size(); i++) {
        if (parts[i] == "meta" || parts[i] == "cmd" || parts[i] == "win") {
            throw Failure{"computer.bad_step", "meta is the Windows key here, which the shell answers rather than the application; "
                                               "Windows shortcuts use control, as in control+s"};
        }
        auto mod = modifiers.find(parts[i]);
        if (mod == modifiers.end()) throw Failure{"computer.bad_step", parts[i] + " is not a modifier; use shift, control or alt"};
        if (std::find(c.mods.begin(), c.mods.end(), mod->second) == c.mods.end()) c.mods.push_back(mod->second);
    }
    // These reach the shell's switcher and Start instead of the application.
    bool alt = std::find(c.mods.begin(), c.mods.end(), VK_MENU) != c.mods.end();
    bool ctrl = std::find(c.mods.begin(), c.mods.end(), VK_CONTROL) != c.mods.end();
    if ((alt && (c.key.vk == VK_TAB || c.key.vk == VK_ESCAPE)) || (ctrl && c.key.vk == VK_ESCAPE)) {
        throw Failure{"computer.bad_step", chord + " switches applications or opens Start; it is answered by Windows, not by this application"};
    }
    return c;
}

INPUT keyInput(WORD vk, bool extended, bool down) {
    INPUT in{};
    in.type = INPUT_KEYBOARD;
    in.ki.wVk = vk;
    in.ki.wScan = static_cast<WORD>(MapVirtualKeyW(vk, MAPVK_VK_TO_VSC));
    in.ki.dwFlags = (down ? 0 : KEYEVENTF_KEYUP) | (extended ? KEYEVENTF_EXTENDEDKEY : 0);
    return in;
}

INPUT unicodeInput(wchar_t unit, bool down) {
    INPUT in{};
    in.type = INPUT_KEYBOARD;
    in.ki.wScan = unit;
    in.ki.dwFlags = KEYEVENTF_UNICODE | (down ? 0 : KEYEVENTF_KEYUP);
    return in;
}

void send(std::vector<INPUT>& inputs) {
    if (inputs.empty()) return;
    UINT sent = SendInput(static_cast<UINT>(inputs.size()), inputs.data(), sizeof(INPUT));
    inputs.clear();
    if (sent == 0) {
        throw Failure{"computer.failed", screenLocked() ? "the screen is locked, and Windows delivers no input while it is"
                                                        : "Windows refused the input"};
    }
}

void chordDown(std::vector<INPUT>& out, const KeyChord& c) {
    for (WORD m : c.mods) out.push_back(keyInput(m, false, true));
    out.push_back(keyInput(c.key.vk, c.key.extended, true));
}

void chordUp(std::vector<INPUT>& out, const KeyChord& c) {
    out.push_back(keyInput(c.key.vk, c.key.extended, false));
    for (auto m = c.mods.rbegin(); m != c.mods.rend(); ++m) out.push_back(keyInput(*m, false, false));
}

void stillFront(DWORD pid, size_t done, size_t total, const char* unit) {
    if (!isFront(pid)) {
        throw Failure{"computer.needs_front", "the application left the front after " + std::to_string(done) + " of " +
                                                  std::to_string(total) + " " + unit + "; the rest did not go in"};
    }
}

} // namespace

Json typeText(DWORD pid, const std::string& text) {
    std::wstring units = widen(text);
    front(pid);
    std::vector<INPUT> batch;
    size_t chunk = 0;
    for (size_t i = 0; i < units.size(); i++) {
        if (chunk == 0) stillFront(pid, i, units.size(), "characters");
        wchar_t u = units[i];
        if (u == L'\r' && i + 1 < units.size() && units[i + 1] == L'\n') continue;
        if (u == L'\n' || u == L'\r' || u == L'\t') {
            WORD vk = u == L'\t' ? VK_TAB : VK_RETURN;
            batch.push_back(keyInput(vk, false, true));
            batch.push_back(keyInput(vk, false, false));
        } else {
            batch.push_back(unicodeInput(u, true));
            batch.push_back(unicodeInput(u, false));
        }
        if (++chunk == 4) {
            send(batch);
            chunk = 0;
            std::this_thread::sleep_for(std::chrono::milliseconds(5));
        }
    }
    send(batch);
    return Json::object();
}

Json pressKey(DWORD pid, const std::string& chord, int times) {
    KeyChord c = resolve(chord);
    front(pid);
    std::vector<INPUT> batch;
    for (int i = 0; i < std::clamp(times, 1, 200); i++) {
        stillFront(pid, i, times, "presses");
        chordDown(batch, c);
        chordUp(batch, c);
        send(batch);
    }
    return Json::object();
}

// holdKey keeps a key down, which is what a game or a scrubbing control reads
// rather than a press. The key always comes back up.
Json holdKey(DWORD pid, const std::string& chord, double seconds) {
    KeyChord c = resolve(chord);
    front(pid);
    std::vector<INPUT> batch;
    chordDown(batch, c);
    send(batch);
    // Held keys belong to whatever is in front, so leaving the front ends the
    // hold at once rather than adding modifiers to the person's own typing.
    auto until = std::chrono::steady_clock::now() + std::chrono::milliseconds(static_cast<long long>(std::clamp(seconds, 0.0, 30.0) * 1000));
    bool left = false;
    while (std::chrono::steady_clock::now() < until && !(left = !isFront(pid))) {
        std::this_thread::sleep_for(std::chrono::milliseconds(20));
    }
    chordUp(batch, c);
    send(batch);
    if (left) throw Failure{"computer.needs_front", "the application left the front during the hold; the key was released early"};
    return Json::object();
}

void sendChord(const std::string& chord) {
    KeyChord c = resolve(chord);
    std::vector<INPUT> batch;
    chordDown(batch, c);
    chordUp(batch, c);
    send(batch);
}
