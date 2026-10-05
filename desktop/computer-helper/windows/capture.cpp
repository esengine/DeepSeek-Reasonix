#include "helper.h"

#include <wincodec.h>
#include <wrl/client.h>

#include <algorithm>

#ifndef PW_RENDERFULLCONTENT
#define PW_RENDERFULLCONTENT 0x00000002
#endif

using Microsoft::WRL::ComPtr;

namespace {

std::string base64(const BYTE* data, size_t n) {
    static const char table[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string out;
    out.reserve((n + 2) / 3 * 4);
    for (size_t i = 0; i < n; i += 3) {
        uint32_t v = data[i] << 16;
        if (i + 1 < n) v |= data[i + 1] << 8;
        if (i + 2 < n) v |= data[i + 2];
        out += table[(v >> 18) & 63];
        out += table[(v >> 12) & 63];
        out += i + 1 < n ? table[(v >> 6) & 63] : '=';
        out += i + 2 < n ? table[v & 63] : '=';
    }
    return out;
}

void check(HRESULT hr, const char* what) {
    if (FAILED(hr)) {
        char buf[96];
        snprintf(buf, sizeof buf, "%s failed (HRESULT 0x%08lx)", what, static_cast<unsigned long>(hr));
        throw Failure{"computer.capture_failed", buf};
    }
}

// jpeg encodes BGRX rows at quality 0.8, the macOS helper's setting.
std::vector<BYTE> jpeg(const BYTE* pixels, UINT width, UINT height, UINT stride) {
    ComPtr<IWICImagingFactory> factory;
    check(CoCreateInstance(CLSID_WICImagingFactory, nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&factory)), "creating the image encoder");
    ComPtr<IWICBitmap> bitmap;
    check(factory->CreateBitmapFromMemory(width, height, GUID_WICPixelFormat32bppBGR, stride, stride * (height - 1) + width * 4,
                                          const_cast<BYTE*>(pixels), &bitmap), "reading the capture");
    ComPtr<IWICFormatConverter> converted;
    check(factory->CreateFormatConverter(&converted), "converting the capture");
    check(converted->Initialize(bitmap.Get(), GUID_WICPixelFormat24bppBGR, WICBitmapDitherTypeNone, nullptr, 0,
                                WICBitmapPaletteTypeCustom), "converting the capture");

    ComPtr<IStream> stream;
    check(CreateStreamOnHGlobal(nullptr, TRUE, &stream), "allocating the image");
    ComPtr<IWICBitmapEncoder> encoder;
    check(factory->CreateEncoder(GUID_ContainerFormatJpeg, nullptr, &encoder), "creating the JPEG encoder");
    check(encoder->Initialize(stream.Get(), WICBitmapEncoderNoCache), "starting the JPEG");
    ComPtr<IWICBitmapFrameEncode> frame;
    ComPtr<IPropertyBag2> props;
    check(encoder->CreateNewFrame(&frame, &props), "starting the JPEG");
    PROPBAG2 option{};
    option.pstrName = const_cast<LPOLESTR>(L"ImageQuality");
    VARIANT quality;
    VariantInit(&quality);
    quality.vt = VT_R4;
    quality.fltVal = 0.8f;
    props->Write(1, &option, &quality);
    check(frame->Initialize(props.Get()), "starting the JPEG");
    check(frame->SetSize(width, height), "sizing the JPEG");
    WICPixelFormatGUID format = GUID_WICPixelFormat24bppBGR;
    check(frame->SetPixelFormat(&format), "formatting the JPEG");
    check(frame->WriteSource(converted.Get(), nullptr), "encoding the JPEG");
    check(frame->Commit(), "encoding the JPEG");
    check(encoder->Commit(), "encoding the JPEG");

    STATSTG stat{};
    check(stream->Stat(&stat, STATFLAG_NONAME), "measuring the JPEG");
    std::vector<BYTE> out(static_cast<size_t>(stat.cbSize.QuadPart));
    LARGE_INTEGER zero{};
    check(stream->Seek(zero, STREAM_SEEK_SET, nullptr), "reading the JPEG");
    ULONG read = 0;
    check(stream->Read(out.data(), static_cast<ULONG>(out.size()), &read), "reading the JPEG");
    out.resize(read);
    return out;
}

struct Dc {
    HDC screen = GetDC(nullptr);
    HDC mem = CreateCompatibleDC(screen);
    HBITMAP bmp = nullptr;
    ~Dc() {
        if (bmp) DeleteObject(bmp);
        DeleteDC(mem);
        ReleaseDC(nullptr, screen);
    }
};

} // namespace

// capture renders an application's front window by itself, whatever covers
// it. bounds is where the visible frame sits on screen, which is what a point
// in the image maps back to.
Json capture(DWORD pid) {
    std::vector<HWND> wins = appWindows(pid, false);
    if (wins.empty()) throw Failure{"computer.no_window", "the application has no window on screen"};
    HWND hwnd = wins[0];
    RECT full{};
    GetWindowRect(hwnd, &full);
    RECT seen = windowBounds(hwnd);
    int fw = full.right - full.left, fh = full.bottom - full.top;
    if (fw <= 1 || fh <= 1) throw Failure{"computer.no_window", "the application has no window on screen"};

    Dc dc;
    BITMAPINFO bi{};
    bi.bmiHeader.biSize = sizeof bi.bmiHeader;
    bi.bmiHeader.biWidth = fw;
    bi.bmiHeader.biHeight = -fh;
    bi.bmiHeader.biPlanes = 1;
    bi.bmiHeader.biBitCount = 32;
    bi.bmiHeader.biCompression = BI_RGB;
    void* bits = nullptr;
    dc.bmp = CreateDIBSection(dc.mem, &bi, DIB_RGB_COLORS, &bits, nullptr, 0);
    if (!dc.bmp) throw Failure{"computer.capture_failed", "no memory for a capture of this size"};
    HGDIOBJ previous = SelectObject(dc.mem, dc.bmp);
    BOOL printed = PrintWindow(hwnd, dc.mem, PW_RENDERFULLCONTENT);
    SelectObject(dc.mem, previous);
    if (!printed) {
        throw Failure{"computer.capture_failed", screenLocked() ? "the screen is locked, and a locked screen renders no window"
                                                                : "the window would not render itself for a capture"};
    }

    int left = std::clamp<int>(seen.left - full.left, 0, fw - 1);
    int top = std::clamp<int>(seen.top - full.top, 0, fh - 1);
    int width = std::clamp<int>(seen.right - seen.left, 1, fw - left);
    int height = std::clamp<int>(seen.bottom - seen.top, 1, fh - top);
    UINT stride = static_cast<UINT>(fw) * 4;
    const BYTE* origin = static_cast<const BYTE*>(bits) + static_cast<size_t>(top) * stride + static_cast<size_t>(left) * 4;
    std::vector<BYTE> encoded = jpeg(origin, width, height, stride);

    RECT placed{full.left + left, full.top + top, full.left + left + width, full.top + top + height};
    return Json::object()
        .set("data", base64(encoded.data(), encoded.size()))
        .set("mime", "image/jpeg")
        .set("width", width)
        .set("height", height)
        .set("window", static_cast<long long>(reinterpret_cast<uintptr_t>(hwnd)))
        .set("bounds", rectJson(placed));
}
