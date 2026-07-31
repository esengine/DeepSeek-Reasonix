#include <windows.h>

extern BOOL __stdcall DllMain(HANDLE hInstance, DWORD dwReason, LPVOID reserved) {
    return TRUE;
}

int __stdcall Add(int a, int b) {
    return a + b;
}
