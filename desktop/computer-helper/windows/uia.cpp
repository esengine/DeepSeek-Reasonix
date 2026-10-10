#include "helper.h"

#include <ole2.h>
#include <UIAutomation.h>
#include <wrl/client.h>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <functional>
#include <future>
#include <map>
#include <memory>
#include <set>
#include <thread>

using Microsoft::WRL::ComPtr;

namespace {

constexpr int maxNodes = 1500;
constexpr int maxDepth = 40;
// A value longer than this is not read back: comparing it costs more than the
// step it would grade.
constexpr int maxReadback = 1 << 16;
constexpr HRESULT elementGone = static_cast<HRESULT>(0x80040201);   // UIA_E_ELEMENTNOTAVAILABLE
constexpr HRESULT elementDisabled = static_cast<HRESULT>(0x80040200); // UIA_E_ELEMENTNOTENABLED
constexpr HRESULT notSupported = static_cast<HRESULT>(0x80040204);    // UIA_E_NOTSUPPORTED

ComPtr<IUIAutomation> uia;
ComPtr<IUIAutomationTreeWalker> walker;
ComPtr<IUIAutomationCacheRequest> cache;

void need() {
    if (!uia) throw Failure{"computer.unsupported", "UI Automation is not available on this system"};
}

std::string hresult(HRESULT hr) {
    char buf[16];
    snprintf(buf, sizeof buf, "0x%08lx", static_cast<unsigned long>(hr));
    return buf;
}

std::string text(const VARIANT& v) {
    return v.vt == VT_BSTR && v.bstrVal ? narrow(std::wstring(v.bstrVal, SysStringLen(v.bstrVal))) : "";
}

struct Cached {
    VARIANT v;
    Cached(IUIAutomationElement* el, PROPERTYID id) {
        VariantInit(&v);
        el->GetCachedPropertyValue(id, &v);
    }
    ~Cached() { VariantClear(&v); }
    bool flag() const { return v.vt == VT_BOOL && v.boolVal == VARIANT_TRUE; }
    int integer() const { return v.vt == VT_I4 ? v.lVal : 0; }
    std::string str() const { return text(v); }
};

// runtimeKey is an element's identity for as long as it exists: the API hands
// back a new pointer for the same element, but not a new runtime id.
std::string runtimeKey(IUIAutomationElement* el) {
    Cached id(el, UIA_RuntimeIdPropertyId);
    if (id.v.vt != (VT_I4 | VT_ARRAY) || !id.v.parray) return {};
    LONG lo = 0, hi = -1;
    SafeArrayGetLBound(id.v.parray, 1, &lo);
    SafeArrayGetUBound(id.v.parray, 1, &hi);
    std::string key;
    for (LONG i = lo; i <= hi; i++) {
        int part = 0;
        SafeArrayGetElement(id.v.parray, &i, &part);
        key += std::to_string(part) + ".";
    }
    return key;
}

// Refs name elements for the model. One is issued per element and never
// reused, so a ref read earlier cannot come to mean something else.
struct Entry {
    DWORD pid;
    ComPtr<IUIAutomationElement> el;
};
int nextRef = 0;
std::map<std::string, std::string> refByKey;
std::map<std::string, Entry> byRef;

std::string refFor(IUIAutomationElement* el, DWORD pid) {
    std::string key = runtimeKey(el);
    if (!key.empty()) {
        auto found = refByKey.find(std::to_string(pid) + ":" + key);
        if (found != refByKey.end()) return found->second;
    }
    std::string ref = "a" + std::to_string(++nextRef);
    if (!key.empty()) refByKey[std::to_string(pid) + ":" + key] = ref;
    byRef[ref] = Entry{pid, el};
    return ref;
}

ComPtr<IUIAutomationElement> element(const std::string& ref, DWORD pid) {
    auto found = byRef.find(ref);
    if (found == byRef.end()) throw Failure{"computer.unknown_ref", ref + " was never issued; refs come from a snapshot"};
    if (found->second.pid != pid) throw Failure{"computer.unknown_ref", ref + " belongs to another application"};
    int alive = 0;
    if (FAILED(found->second.el->get_CurrentProcessId(&alive))) {
        throw Failure{"computer.stale_ref", ref + " is no longer on screen; take a new snapshot"};
    }
    return found->second.el;
}

std::string roleName(int type) {
    static const char* names[] = {
        "button", "calendar", "checkBox", "comboBox", "edit", "hyperlink", "image", "listItem", "list", "menu",
        "menuBar", "menuItem", "progressBar", "radioButton", "scrollBar", "slider", "spinner", "statusBar", "tab",
        "tabItem", "text", "toolBar", "toolTip", "tree", "treeItem", "custom", "group", "thumb", "dataGrid",
        "dataItem", "document", "splitButton", "window", "pane", "header", "headerItem", "table", "titleBar",
        "separator", "semanticZoom", "appBar",
    };
    int i = type - UIA_ButtonControlTypeId;
    return i >= 0 && i < static_cast<int>(std::size(names)) ? names[i] : "element";
}

std::string clipQuote(const std::string& s) {
    std::wstring w = widen(s);
    if (w.size() > 160) {
        size_t cut = 160;
        if (IS_HIGH_SURROGATE(w[cut - 1])) cut--;
        return quote(narrow(w.substr(0, cut)) + "\xE2\x80\xA6");
    }
    return quote(s);
}

bool pressable(IUIAutomationElement* el) {
    return Cached(el, UIA_IsInvokePatternAvailablePropertyId).flag() || Cached(el, UIA_IsTogglePatternAvailablePropertyId).flag() ||
           Cached(el, UIA_IsExpandCollapsePatternAvailablePropertyId).flag() ||
           Cached(el, UIA_IsSelectionItemPatternAvailablePropertyId).flag() ||
           !Cached(el, UIA_LegacyIAccessibleDefaultActionPropertyId).str().empty();
}

// Containers that say nothing when unnamed: their children stand in for them.
bool transparent(int type) {
    return type == UIA_GroupControlTypeId || type == UIA_PaneControlTypeId || type == UIA_CustomControlTypeId;
}

std::string windowTitle(HWND hwnd) {
    wchar_t buf[512];
    int n = GetWindowTextW(hwnd, buf, ARRAYSIZE(buf));
    return narrow(std::wstring(buf, n > 0 ? n : 0));
}

HWND cachedHandle(IUIAutomationElement* el) {
    return reinterpret_cast<HWND>(static_cast<intptr_t>(Cached(el, UIA_NativeWindowHandlePropertyId).integer()));
}

// inWindowModal is a dialog drawn inside its window that declares itself
// modal. A top-level window's own root does not count: one holds input only
// through the owner it has disabled.
bool inWindowModal(IUIAutomationElement* el, HWND root) {
    return Cached(el, UIA_ControlTypePropertyId).integer() == UIA_WindowControlTypeId &&
           Cached(el, UIA_WindowIsModalPropertyId).flag() && cachedHandle(el) != root;
}

// modalJson names a modal for the model; the titles are shown, never read.
Json modalJson(IUIAutomationElement* el, DWORD pid, const std::string& blocks) {
    return Json::object().set("ref", refFor(el, pid)).set("title", Cached(el, UIA_NamePropertyId).str()).set("blocks", blocks);
}

// win32Modal is the modal dialog hwnd is, when it holds another window of
// the process; null otherwise.
Json win32Modal(HWND hwnd, DWORD pid) {
    HWND held = heldOwner(hwnd, pid);
    if (!held) return Json();
    ComPtr<IUIAutomationElement> el;
    if (FAILED(uia->ElementFromHandleBuildCache(hwnd, cache.Get(), &el)) || !el) {
        return Json::object().set("ref", "").set("title", windowTitle(hwnd)).set("blocks", windowTitle(held));
    }
    return modalJson(el.Get(), pid, windowTitle(held));
}

struct Walk {
    DWORD pid;
    HWND root = nullptr;
    bool rootIsModal = false;
    std::string rootTitle, heldTitle;
    Json modals = Json::array();
    std::set<std::string> modalRefs;
    std::vector<std::string> lines;
    std::set<std::string> seen;
    int count = 0;
    // A tree that hands back elements already seen, or nests unnamed
    // containers without end, is bounded by work done, not by lines written.
    int steps = 0;
    int nesting = 0;
    static constexpr int maxSteps = maxNodes * 4;
    static constexpr int maxNesting = 200;

    void children(IUIAutomationElement* el, int depth) {
        ComPtr<IUIAutomationElement> child;
        walker->GetFirstChildElementBuildCache(el, cache.Get(), &child);
        while (child && count < maxNodes && ++steps < maxSteps) {
            visit(child.Get(), depth);
            ComPtr<IUIAutomationElement> next;
            walker->GetNextSiblingElementBuildCache(child.Get(), cache.Get(), &next);
            child = next;
        }
    }

    void visit(IUIAutomationElement* el, int depth) {
        if (count >= maxNodes || depth > maxDepth || nesting >= maxNesting) return;
        struct Nest { int& n; Nest(int& n) : n(++n) {} ~Nest() { --n; } } nest(nesting);
        std::string key = runtimeKey(el);
        if (!key.empty() && !seen.insert(key).second) return;
        count++;
        int type = Cached(el, UIA_ControlTypePropertyId).integer();
        std::string label = Cached(el, UIA_NamePropertyId).str();
        bool password = Cached(el, UIA_IsPasswordPropertyId).flag();
        std::string value = password ? "" : Cached(el, UIA_ValueValuePropertyId).str();
        if (label.empty() && type == UIA_TextControlTypeId) label = value;
        if (transparent(type) && label.empty()) {
            children(el, depth);
            return;
        }
        std::string line(static_cast<size_t>(depth) * 2, ' ');
        line += "- " + roleName(type);
        if (!label.empty()) line += " " + clipQuote(label);
        line += " [" + refFor(el, pid) + "]";
        if (!value.empty() && value != label && type != UIA_TextControlTypeId) line += " value=" + clipQuote(value);
        std::string help = Cached(el, UIA_HelpTextPropertyId).str();
        if (value.empty() && !help.empty() && (type == UIA_EditControlTypeId || type == UIA_ComboBoxControlTypeId)) {
            line += " placeholder=" + clipQuote(help);
        }
        if (password) line += " password";
        if (Cached(el, UIA_HasKeyboardFocusPropertyId).flag()) line += " focused";
        if (!Cached(el, UIA_IsEnabledPropertyId).flag()) line += " disabled";
        if (Cached(el, UIA_SelectionItemIsSelectedPropertyId).flag()) line += " selected";
        if (Cached(el, UIA_ToggleToggleStatePropertyId).integer() == ToggleState_On) line += " checked";
        if (pressable(el)) line += " pressable";
        bool isRoot = cachedHandle(el) == root;
        if ((isRoot && rootIsModal) || inWindowModal(el, root)) {
            line += " modal";
            Json m = modalJson(el, pid, isRoot ? heldTitle : rootTitle);
            if (modalRefs.insert(m.get("ref")->string()).second) modals.push(m);
        }
        lines.push_back(line);
        children(el, depth + 1);
    }
};

// settle reads what an accessibility call's result says about the element.
void settle(HRESULT hr, const std::string& what, const char* action) {
    if (SUCCEEDED(hr)) return;
    if (hr == elementGone) throw Failure{"computer.stale_ref", what + " is no longer on screen; take a new snapshot"};
    if (hr == elementDisabled) throw Failure{"computer.no_action", what + " is disabled"};
    if (hr == notSupported) throw Failure{"computer.no_action", what + " takes no " + action};
    throw Failure{"computer.failed", std::string(action) + " on " + what + " failed (HRESULT " + hresult(hr) + ")"};
}

// detached runs an action that can block until the application finishes what
// it starts — an invoke that opens a modal dialog returns when the dialog
// closes. Past a short wait the action has been delivered, which is the answer.
HRESULT detached(std::function<HRESULT()> action) {
    auto done = std::make_shared<std::promise<HRESULT>>();
    std::future<HRESULT> result = done->get_future();
    std::thread([action = std::move(action), done] {
        CoInitializeEx(nullptr, COINIT_MULTITHREADED);
        done->set_value(action());
        CoUninitialize();
    }).detach();
    if (result.wait_for(std::chrono::milliseconds(1500)) == std::future_status::timeout) return S_OK;
    return result.get();
}

template <typename P>
ComPtr<P> pattern(IUIAutomationElement* el, PATTERNID id) {
    ComPtr<P> p;
    el->GetCurrentPatternAs(id, IID_PPV_ARGS(&p));
    return p;
}

// activate does what a click on the element would: invoke it, or toggle,
// expand, select, or its accessible default action, in that order.
void activate(IUIAutomationElement* el, const std::string& what) {
    if (auto p = pattern<IUIAutomationInvokePattern>(el, UIA_InvokePatternId)) {
        return settle(detached([p] { return p->Invoke(); }), what, "invoke");
    }
    if (auto p = pattern<IUIAutomationTogglePattern>(el, UIA_TogglePatternId)) {
        return settle(detached([p] { return p->Toggle(); }), what, "toggle");
    }
    if (auto p = pattern<IUIAutomationExpandCollapsePattern>(el, UIA_ExpandCollapsePatternId)) {
        ExpandCollapseState state = ExpandCollapseState_LeafNode;
        p->get_CurrentExpandCollapseState(&state);
        bool open = state == ExpandCollapseState_Collapsed;
        return settle(detached([p, open] { return open ? p->Expand() : p->Collapse(); }), what, "expand");
    }
    if (auto p = pattern<IUIAutomationSelectionItemPattern>(el, UIA_SelectionItemPatternId)) {
        return settle(detached([p] { return p->Select(); }), what, "select");
    }
    if (auto p = pattern<IUIAutomationLegacyIAccessiblePattern>(el, UIA_LegacyIAccessiblePatternId)) {
        BSTR action = nullptr;
        p->get_CurrentDefaultAction(&action);
        bool has = action && SysStringLen(action) > 0;
        SysFreeString(action);
        if (has) return settle(detached([p] { return p->DoDefaultAction(); }), what, "default action");
    }
    throw Failure{"computer.no_action", what + " takes no press"};
}

// showAt puts the agent's cursor on an element before it is acted on.
void showAt(IUIAutomationElement* el) {
    RECT r{};
    if (SUCCEEDED(el->get_CurrentBoundingRectangle(&r)) && r.right > r.left && r.bottom > r.top) {
        cursorMove(POINT{(r.left + r.right) / 2, (r.top + r.bottom) / 2});
    }
}

void checkPoint(DWORD pid, POINT at) {
    std::string where = "(" + std::to_string(at.x) + ", " + std::to_string(at.y) + ")";
    HWND hit = WindowFromPoint(at);
    if (!hit || contentPid(GetAncestor(hit, GA_ROOT)) != pid) {
        throw Failure{"computer.no_element", hit ? "the element at " + where + " belongs to another application"
                                                 : "nothing of this application is at " + where};
    }
}

// scrollable is the nearest element, from el outwards, that scrolls vertically.
ComPtr<IUIAutomationScrollPattern> scrollable(ComPtr<IUIAutomationElement> el) {
    for (int i = 0; el && i < maxDepth; i++) {
        if (auto p = pattern<IUIAutomationScrollPattern>(el.Get(), UIA_ScrollPatternId)) {
            BOOL can = FALSE;
            if (SUCCEEDED(p->get_CurrentVerticallyScrollable(&can)) && can) return p;
        }
        ComPtr<IUIAutomationElement> parent;
        walker->GetParentElement(el.Get(), &parent);
        el = parent;
    }
    return nullptr;
}

// wheel turns the wheel over the application's front window without the
// pointer: the message goes to the innermost window at its centre.
void wheel(DWORD pid, double amount) {
    std::vector<HWND> wins = appWindows(pid, false);
    if (wins.empty()) throw Failure{"computer.no_window", "the application has no window on screen to scroll"};
    RECT r = windowBounds(wins[0]);
    POINT centre{(r.left + r.right) / 2, (r.top + r.bottom) / 2};
    HWND target = wins[0];
    for (int i = 0; i < maxDepth; i++) {
        POINT local = centre;
        ScreenToClient(target, &local);
        HWND child = ChildWindowFromPointEx(target, local, CWP_SKIPINVISIBLE | CWP_SKIPDISABLED | CWP_SKIPTRANSPARENT);
        if (!child || child == target) break;
        target = child;
    }
    int delta = static_cast<int>(amount * WHEEL_DELTA / 3);
    if (delta > -WHEEL_DELTA && delta < WHEEL_DELTA) delta = amount < 0 ? -WHEEL_DELTA : WHEEL_DELTA;
    PostMessageW(target, WM_MOUSEWHEEL, MAKEWPARAM(0, static_cast<SHORT>(delta)), MAKELPARAM(centre.x, centre.y));
}

// nativeRoot is the top-level window an element is drawn in.
HWND nativeRoot(IUIAutomationElement* el) {
    ComPtr<IUIAutomationElement> at = el;
    for (int i = 0; at && i < maxDepth; i++) {
        UIA_HWND h = nullptr;
        if (SUCCEEDED(at->get_CurrentNativeWindowHandle(&h)) && h) return GetAncestor(static_cast<HWND>(h), GA_ROOT);
        ComPtr<IUIAutomationElement> parent;
        walker->GetParentElement(at.Get(), &parent);
        at = parent;
    }
    return nullptr;
}

// reachable refuses an element in a window the application has disabled,
// which a modal dialog does to the window it holds: the application has
// declared that window takes no input until the dialog is answered.
void reachable(IUIAutomationElement* el, DWORD pid, const std::string& what) {
    HWND root = nativeRoot(el);
    if (!root || IsWindowEnabled(root)) return;
    for (HWND w : appWindows(pid, false)) {
        for (HWND o = heldOwner(w, pid) ? GetWindow(w, GW_OWNER) : nullptr; o; o = GetWindow(o, GW_OWNER)) {
            if (o == root) {
                throw Failure{"computer.blocked", what + " is in a window a modal dialog holds; answer or dismiss the dialog first",
                              win32Modal(w, pid)};
            }
        }
    }
    throw Failure{"computer.blocked", what + " is in a window the application has disabled"};
}

// typedExactly is whether after is before with typed put in at one place,
// replacing whatever was selected there. Where repeated characters make the
// changed span ambiguous it is taken from either end, and any other change,
// such as autocomplete or a single-line field dropping a newline, is not it.
bool typedExactly(const std::wstring& before, const std::wstring& after, const std::wstring& typed) {
    if (after == before || after.size() < typed.size()) return false;
    auto span = [&](bool prefixFirst) {
        size_t p = 0, s = 0, room = std::min(before.size(), after.size());
        auto prefix = [&] { while (p < room - s && before[p] == after[p]) p++; };
        auto suffix = [&] { while (s < room - p && before[before.size() - 1 - s] == after[after.size() - 1 - s]) s++; };
        if (prefixFirst) {
            prefix();
            suffix();
        } else {
            suffix();
            prefix();
        }
        return after.substr(p, after.size() - p - s);
    };
    return span(true) == typed || span(false) == typed;
}

std::wstring flat(std::wstring s) {
    s.erase(std::remove(s.begin(), s.end(), L'\r'), s.end());
    return s;
}

// readValue is what an element holds as text, for comparing before and after
// a step. A password, or a value too long to compare, is not read.
bool readValue(IUIAutomationElement* el, std::wstring& out) {
    BOOL password = FALSE;
    if (FAILED(el->get_CurrentIsPassword(&password)) || password) return false;
    BSTR v = nullptr;
    if (auto p = pattern<IUIAutomationValuePattern>(el, UIA_ValuePatternId)) {
        if (FAILED(p->get_CurrentValue(&v))) return false;
    } else if (auto t = pattern<IUIAutomationTextPattern>(el, UIA_TextPatternId)) {
        ComPtr<IUIAutomationTextRange> doc;
        if (FAILED(t->get_DocumentRange(&doc)) || !doc || FAILED(doc->GetText(maxReadback, &v))) return false;
    } else {
        return false;
    }
    out = flat(v ? std::wstring(v, SysStringLen(v)) : std::wstring());
    SysFreeString(v);
    return out.size() < static_cast<size_t>(maxReadback);
}

} // namespace

Json effect(const char* cls, const char* evidence, const Json& modal) {
    Json e = Json::object().set("class", cls);
    if (evidence) {
        Json kinds = Json::array();
        kinds.push(evidence);
        e.set("evidence", kinds);
    }
    if (modal.kind() != Json::Kind::Null) e.set("blocked_by", modal);
    return Json::object().set("effect", e);
}

Landing landing(DWORD pid, bool withValue) {
    need();
    Landing at;
    ComPtr<IUIAutomationElement> focused;
    int owner = 0;
    if (FAILED(uia->GetFocusedElementBuildCache(cache.Get(), &focused)) || !focused || FAILED(focused->get_CurrentProcessId(&owner)) ||
        static_cast<DWORD>(owner) != pid) {
        focused = nullptr;
    }
    HWND root = focused ? nativeRoot(focused.Get()) : nullptr;
    if (!root && isFront(pid)) root = GetForegroundWindow();
    ComPtr<IUIAutomationElement> el = focused;
    for (int i = 0; el && i < maxDepth && at.modal.kind() == Json::Kind::Null; i++) {
        if (inWindowModal(el.Get(), root)) at.modal = modalJson(el.Get(), pid, windowTitle(root));
        ComPtr<IUIAutomationElement> parent;
        walker->GetParentElementBuildCache(el.Get(), cache.Get(), &parent);
        el = parent;
    }
    if (at.modal.kind() == Json::Kind::Null && root) at.modal = win32Modal(root, pid);
    if (!focused || !withValue) return at;
    if (Cached(focused.Get(), UIA_IsValuePatternAvailablePropertyId).flag()) {
        at.takesText = !Cached(focused.Get(), UIA_ValueIsReadOnlyPropertyId).flag();
    } else {
        int type = Cached(focused.Get(), UIA_ControlTypePropertyId).integer();
        at.takesText = type == UIA_EditControlTypeId || type == UIA_DocumentControlTypeId;
    }
    at.read = [focused](std::wstring& out) { return readValue(focused.Get(), out); };
    at.readable = at.read(at.before);
    return at;
}

void refuseHeldText(const Landing& at) {
    if (at.modal.kind() == Json::Kind::Null || at.takesText) return;
    throw Failure{"computer.blocked",
                  "a modal dialog holds this application's input and its focused element takes no text, where a letter can answer the "
                  "dialog; answer or dismiss it first",
                  at.modal};
}

// The application takes typed characters one message at a time, so a value
// read part-way through holds some of them; it is read again until all are in.
Json typedEffect(const Landing& at, const std::wstring& typed) {
    if (!at.readable) return effect("unverifiable", nullptr, at.modal);
    std::wstring after, want = flat(typed);
    for (int i = 0; i < 10; i++) {
        if (!at.read(after)) return effect("unverifiable", nullptr, at.modal);
        if (typedExactly(at.before, after, want)) return effect("confirmed", "value_readback", at.modal);
        std::this_thread::sleep_for(std::chrono::milliseconds(40));
    }
    if (after == at.before) return effect("suspected_noop", "value_unchanged", at.modal);
    return effect("unverifiable", nullptr, at.modal);
}

void uiaStart() {
    if (FAILED(CoCreateInstance(__uuidof(CUIAutomation8), nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&uia))) &&
        FAILED(CoCreateInstance(__uuidof(CUIAutomation), nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&uia)))) {
        return;
    }
    // A hung application must not hang the helper with it.
    ComPtr<IUIAutomation2> timed;
    if (SUCCEEDED(uia.As(&timed))) {
        timed->put_ConnectionTimeout(2000);
        timed->put_TransactionTimeout(5000);
    }
    uia->get_ControlViewWalker(&walker);
    uia->CreateCacheRequest(&cache);
    for (PROPERTYID id : {UIA_RuntimeIdPropertyId, UIA_ControlTypePropertyId, UIA_NamePropertyId, UIA_ValueValuePropertyId,
                          UIA_IsPasswordPropertyId, UIA_HelpTextPropertyId, UIA_HasKeyboardFocusPropertyId, UIA_IsEnabledPropertyId,
                          UIA_SelectionItemIsSelectedPropertyId, UIA_ToggleToggleStatePropertyId, UIA_IsInvokePatternAvailablePropertyId,
                          UIA_IsTogglePatternAvailablePropertyId, UIA_IsExpandCollapsePatternAvailablePropertyId,
                          UIA_IsSelectionItemPatternAvailablePropertyId, UIA_LegacyIAccessibleDefaultActionPropertyId,
                          UIA_WindowIsModalPropertyId, UIA_NativeWindowHandlePropertyId, UIA_IsValuePatternAvailablePropertyId,
                          UIA_ValueIsReadOnlyPropertyId}) {
        cache->AddProperty(id);
    }
}

Json snapshot(DWORD pid) {
    need();
    Walk walk{pid};
    std::string note;
    std::vector<HWND> wins = appWindows(pid, false);
    if (wins.empty()) {
        note = screenLocked()
                   ? "The screen is locked. While it is, no application's window can be read, clicked by ref or hit by point; unlocking the screen brings it back."
                   : "This application has no window on screen right now — it may be minimized. A type, key or pointer step brings it forward; take a new snapshot after.";
    }
    for (HWND hwnd : wins) {
        walk.root = hwnd;
        HWND held = heldOwner(hwnd, pid);
        walk.rootIsModal = held != nullptr;
        walk.rootTitle = windowTitle(hwnd);
        walk.heldTitle = held ? windowTitle(held) : "";
        ComPtr<IUIAutomationElement> root;
        if (SUCCEEDED(uia->ElementFromHandleBuildCache(hwnd, cache.Get(), &root)) && root) walk.visit(root.Get(), 0);
    }
    Json lines = Json::array();
    for (auto& l : walk.lines) lines.push(std::move(l));
    return Json::object()
        .set("app", displayName(pid))
        .set("bundle", identity(pid))
        .set("lines", lines)
        .set("truncated", walk.count >= maxNodes)
        .set("note", note)
        .set("modals", walk.modals);
}

Json press(DWORD pid, const std::string& ref) {
    need();
    ComPtr<IUIAutomationElement> el = element(ref, pid);
    reachable(el.Get(), pid, ref);
    showAt(el.Get());
    activate(el.Get(), ref);
    return Json::object();
}

// clickAt is a press at a point: the element there is asked for its action,
// so the person's pointer never moves. One with no action says so rather than
// being clicked some other way.
Json clickAt(DWORD pid, double x, double y) {
    need();
    POINT at{static_cast<LONG>(x), static_cast<LONG>(y)};
    cursorMove(at);
    // Checked after the glide, just before the lookup: a window that came up
    // over the point meanwhile is not the application that was approved.
    checkPoint(pid, at);
    ComPtr<IUIAutomationElement> el;
    if (FAILED(uia->ElementFromPointBuildCache(at, cache.Get(), &el)) || !el) {
        throw Failure{"computer.no_element", "nothing of this application is at (" + num(x) + ", " + num(y) + ")"};
    }
    int owner = 0;
    DWORD framePid = 0;
    GetWindowThreadProcessId(GetAncestor(WindowFromPoint(at), GA_ROOT), &framePid);
    if (FAILED(el->get_CurrentProcessId(&owner)) || (static_cast<DWORD>(owner) != pid && static_cast<DWORD>(owner) != framePid)) {
        throw Failure{"computer.no_element", "the element at (" + num(x) + ", " + num(y) + ") belongs to another application"};
    }
    int type = Cached(el.Get(), UIA_ControlTypePropertyId).integer();
    std::string what = "the " + roleName(type) + " at that point";
    reachable(el.Get(), pid, what);
    if (pressable(el.Get())) {
        activate(el.Get(), what);
    } else if (type == UIA_EditControlTypeId || type == UIA_ComboBoxControlTypeId || type == UIA_DocumentControlTypeId) {
        settle(el->SetFocus(), what, "focus");
    } else {
        throw Failure{"computer.no_action", what + " takes no accessibility action; it needs the real pointer"};
    }
    return Json::object().set("role", roleName(type));
}

// menu asks an element for its context menu, which is what a right click is
// for. Read the application again afterwards to see the menu.
Json menu(DWORD pid, const std::string& ref) {
    need();
    ComPtr<IUIAutomationElement> target = element(ref, pid);
    reachable(target.Get(), pid, ref);
    showAt(target.Get());
    ComPtr<IUIAutomationElement3> el;
    if (FAILED(target.As(&el))) throw Failure{"computer.no_action", ref + " has no context menu"};
    HRESULT hr = detached([el] { return el->ShowContextMenu(); });
    if (FAILED(hr) && hr != elementGone) throw Failure{"computer.no_action", ref + " has no context menu"};
    settle(hr, ref, "context menu");
    return Json::object();
}

Json scroll(DWORD pid, const std::string& ref, double amount) {
    need();
    ComPtr<IUIAutomationElement> from;
    if (!ref.empty()) {
        from = element(ref, pid);
        reachable(from.Get(), pid, ref);
        showAt(from.Get());
        if (amount == 0) {
            BOOL offscreen = TRUE;
            if (SUCCEEDED(from->get_CurrentIsOffscreen(&offscreen)) && !offscreen) return Json::object().set("how", "revealed");
            if (auto p = pattern<IUIAutomationScrollItemPattern>(from.Get(), UIA_ScrollItemPatternId)) {
                settle(p->ScrollIntoView(), ref, "scroll into view");
                return Json::object().set("how", "revealed");
            }
        }
    }
    if (amount == 0) {
        throw Failure{"computer.bad_step", "a scroll needs a ref that can be revealed, or lines to turn the wheel by"};
    }
    if (!from) {
        ComPtr<IUIAutomationElement> focused;
        int owner = 0;
        if (SUCCEEDED(uia->GetFocusedElement(&focused)) && focused && SUCCEEDED(focused->get_CurrentProcessId(&owner)) &&
            static_cast<DWORD>(owner) == pid) {
            from = focused;
        }
    }
    if (auto p = scrollable(from)) {
        ScrollAmount step = amount < 0 ? ScrollAmount_SmallIncrement : ScrollAmount_SmallDecrement;
        for (int i = 0; i < static_cast<int>(std::abs(amount)); i++) settle(p->Scroll(ScrollAmount_NoAmount, step), "the view", "scroll");
    } else {
        wheel(pid, amount);
    }
    return Json::object().set("how", "wheel");
}

Json focus(DWORD pid, const std::string& ref) {
    need();
    ComPtr<IUIAutomationElement> el = element(ref, pid);
    reachable(el.Get(), pid, ref);
    showAt(el.Get());
    HRESULT hr = el->SetFocus();
    if (FAILED(hr) && hr != elementGone) throw Failure{"computer.no_action", ref + " cannot take focus (HRESULT " + hresult(hr) + ")"};
    settle(hr, ref, "focus");
    return Json::object();
}

Json setValue(DWORD pid, const std::string& ref, const std::string& value) {
    need();
    ComPtr<IUIAutomationElement> el = element(ref, pid);
    reachable(el.Get(), pid, ref);
    showAt(el.Get());
    auto p = pattern<IUIAutomationValuePattern>(el.Get(), UIA_ValuePatternId);
    BOOL readOnly = FALSE;
    if (!p || (SUCCEEDED(p->get_CurrentIsReadOnly(&readOnly)) && readOnly)) {
        throw Failure{"computer.no_action", ref + " has no value that can be set"};
    }
    std::wstring before, now;
    bool hadBefore = readValue(el.Get(), before);
    BSTR b = SysAllocString(widen(value).c_str());
    HRESULT hr = p->SetValue(b);
    SysFreeString(b);
    if (FAILED(hr) && hr != elementGone) throw Failure{"computer.no_action", ref + " has no value that can be set (HRESULT " + hresult(hr) + ")"};
    settle(hr, ref, "set value");
    if (!readValue(el.Get(), now)) return effect("unverifiable", nullptr, Json());
    if (now == flat(widen(value))) return effect("confirmed", "value_readback", Json());
    if (hadBefore && now == before) return effect("suspected_noop", "value_unchanged", Json());
    return effect("unverifiable", nullptr, Json());
}
