#pragma once

#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>

#include <string>
#include <vector>

#include "json.h"

// Failure is an operation that did not happen, answered with the same codes the
// macOS helper uses so the kernel reads both alike.
struct Failure {
    std::string code;
    std::string message;
};

std::string narrow(const std::wstring& w);
std::wstring widen(const std::string& s);
std::string lower(std::string s);
std::string num(double v);

DWORD pidParam(const Json& params);
std::string stringParam(const Json& params, const char* key);
double numberParam(const Json& params, const char* key);
double numberOr(const Json& params, const char* key, double fallback);

// main.cpp: one line to the kernel, from whichever thread has one to say.
void reply(const Json& body);

// cursor.cpp: the agent's own cursor, and Escape while it shows.
void cursorStart();
void cursorMove(POINT point);
void cursorHide();

// process.cpp: which application a process or window is, and its windows.
std::string identity(DWORD pid);
std::string exeName(DWORD pid);
std::string displayName(DWORD pid);
DWORD contentPid(HWND top);
std::vector<HWND> appWindows(DWORD pid, bool includeMinimized);
bool appListed(HWND top);
void requireOperable(DWORD pid);
bool ownsPoint(DWORD pid, POINT at);
bool isFront(DWORD pid);
void front(DWORD pid);
RECT windowBounds(HWND hwnd);
Json rectJson(const RECT& r);

// apps.cpp
Json listApps();

// screen.cpp
bool screenLocked();
Json status();

// uia.cpp
void uiaStart();
Json snapshot(DWORD pid);
Json press(DWORD pid, const std::string& ref);
Json clickAt(DWORD pid, double x, double y);
Json menu(DWORD pid, const std::string& ref);
Json scroll(DWORD pid, const std::string& ref, double amount);
Json focus(DWORD pid, const std::string& ref);
Json setValue(DWORD pid, const std::string& ref, const std::string& text);

// keyboard.cpp
Json typeText(DWORD pid, const std::string& text);
Json pressKey(DWORD pid, const std::string& chord, int times);
Json holdKey(DWORD pid, const std::string& chord, double seconds);
void sendChord(const std::string& chord);

// pointer.cpp
Json pointerMove(DWORD pid, POINT to);
Json pointerClick(DWORD pid, POINT at, const std::string& button, int clicks);
Json pointerDrag(DWORD pid, POINT from, POINT to);
Json pointerPosition();
Json pointerRelease();

// capture.cpp
Json capture(DWORD pid);

// clipboard.cpp
Json paste(DWORD pid, const std::string& text);
void clipboardSettle();
