#include "helper.h"

#include <chrono>
#include <cstring>
#include <mutex>
#include <thread>

// paste puts text where the person is typing without typing it: a long value
// arrives at once, and an application that completes or reformats what it is
// given sees one insertion instead of a hundred keystrokes.
//
// The clipboard is the person's. What was on it goes back, unless something
// else claimed it while this ran — then theirs is the newer one and stays.

namespace {

struct Saved {
    UINT format;
    std::vector<BYTE> data;
};

std::mutex restoring;
std::thread pending;

bool open() {
    for (int i = 0; i < 20; i++) {
        if (OpenClipboard(nullptr)) return true;
        std::this_thread::sleep_for(std::chrono::milliseconds(25));
    }
    return false;
}

// Formats held as GDI handles rather than memory cannot be copied byte for
// byte; each has a memory twin Windows synthesises it back from.
bool copyable(UINT format) {
    return format != CF_BITMAP && format != CF_ENHMETAFILE && format != CF_METAFILEPICT && format != CF_PALETTE &&
           format != CF_OWNERDISPLAY && format != CF_DSPBITMAP && format != CF_DSPENHMETAFILE && format != CF_DSPMETAFILEPICT;
}

std::vector<Saved> save() {
    std::vector<Saved> out;
    for (UINT f = EnumClipboardFormats(0); f; f = EnumClipboardFormats(f)) {
        if (!copyable(f)) continue;
        HANDLE h = GetClipboardData(f);
        if (!h) continue;
        SIZE_T n = GlobalSize(h);
        const BYTE* p = static_cast<const BYTE*>(GlobalLock(h));
        if (!p) continue;
        out.push_back({f, std::vector<BYTE>(p, p + n)});
        GlobalUnlock(h);
    }
    return out;
}

void put(UINT format, const void* data, SIZE_T n) {
    HGLOBAL h = GlobalAlloc(GMEM_MOVEABLE, n ? n : 1);
    if (!h) return;
    void* p = GlobalLock(h);
    if (n) memcpy(p, data, n);
    GlobalUnlock(h);
    if (!SetClipboardData(format, h)) GlobalFree(h);
}

// The agent's text stays out of the person's clipboard history and cloud sync.
void keepPrivate() {
    DWORD no = 0;
    put(RegisterClipboardFormatW(L"CanIncludeInClipboardHistory"), &no, sizeof no);
    put(RegisterClipboardFormatW(L"CanUploadToCloudClipboard"), &no, sizeof no);
    put(RegisterClipboardFormatW(L"ExcludeClipboardContentFromMonitorProcessing"), &no, sizeof no);
}

void restore(std::vector<Saved> saved, DWORD written) {
    std::this_thread::sleep_for(std::chrono::milliseconds(1500));
    if (GetClipboardSequenceNumber() != written || !open()) return;
    if (GetClipboardSequenceNumber() != written) {
        CloseClipboard();
        return;
    }
    EmptyClipboard();
    for (const Saved& s : saved) put(s.format, s.data.data(), s.data.size());
    CloseClipboard();
}

} // namespace

void clipboardSettle() {
    std::lock_guard<std::mutex> hold(restoring);
    if (pending.joinable()) pending.join();
}

Json paste(DWORD pid, const std::string& text) {
    front(pid);
    clipboardSettle();
    if (!open()) throw Failure{"computer.failed", "another application is holding the clipboard"};
    std::vector<Saved> saved = save();
    EmptyClipboard();
    std::wstring w = widen(text);
    put(CF_UNICODETEXT, w.c_str(), (w.size() + 1) * sizeof(wchar_t));
    keepPrivate();
    CloseClipboard();
    // However this returns, the person's clipboard is owed back.
    struct GiveBack {
        std::vector<Saved> saved;
        DWORD written;
        ~GiveBack() {
            std::lock_guard<std::mutex> hold(restoring);
            pending = std::thread(restore, std::move(saved), written);
        }
    } giveBack{std::move(saved), GetClipboardSequenceNumber()};
    if (!isFront(pid)) throw Failure{"computer.needs_front", "the application left the front before the paste; nothing was pasted"};
    sendChord("control+v");
    // The application reads the clipboard when it gets round to the keystroke,
    // and giving it back before then hands it the person's clipboard instead.
    std::this_thread::sleep_for(std::chrono::milliseconds(250));
    return Json::object().set("pasted", static_cast<unsigned long long>(w.size()));
}
