#include "helper.h"

// screenLocked is whether the input desktop is somewhere other than the
// person's own: the lock screen and the secure desktop both take it, and while
// either does no input or capture reaches an application.
bool screenLocked() {
    HDESK desk = OpenInputDesktop(0, FALSE, DESKTOP_READOBJECTS);
    if (!desk) return true;
    wchar_t name[64] = {};
    DWORD needed = 0;
    bool own = GetUserObjectInformationW(desk, UOI_NAME, name, sizeof name, &needed) && _wcsicmp(name, L"Default") == 0;
    CloseDesktop(desk);
    return !own;
}

// Windows asks for no grant before one process reads another's accessibility
// tree or captures its window, so both are always on here.
Json status() {
    return Json::object().set("accessibility", true).set("screen_recording", true).set("screen_locked", screenLocked());
}
