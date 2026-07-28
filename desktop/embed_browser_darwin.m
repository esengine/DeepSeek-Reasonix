//go:build darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <stdlib.h>
#import <string.h>

extern void ReasonixEmbedBrowserEmitState(const char *url, const char *title, int canBack, int canForward, int loading);
extern void ReasonixEmbedBrowserEmitError(const char *message);
extern void ReasonixEmbedBrowserSnapshotDone(const char *dataURLOrError);
extern void ReasonixEmbedBrowserEmitPick(double x, double y, double w, double h, const char *selector, const char *tagName,
                                        const char *text);

// WKUserContentController retains script message handlers strongly; use a weak
// proxy so the controller can be released without a retain cycle.
@interface ReasonixWeakScriptMessageHandler : NSObject <WKScriptMessageHandler>
@property(nonatomic, weak) id<WKScriptMessageHandler> target;
@end

@implementation ReasonixWeakScriptMessageHandler
- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    id<WKScriptMessageHandler> target = self.target;
    if (target != nil) {
        [target userContentController:userContentController didReceiveScriptMessage:message];
    }
}
@end

@interface ReasonixEmbedBrowserController : NSObject <WKNavigationDelegate, WKScriptMessageHandler>
@property(nonatomic, strong) WKWebView *webView;
@property(nonatomic, assign) BOOL visible;
@property(nonatomic, assign) BOOL pickMode;
@property(nonatomic, copy) NSString *pickAccent;
@property(nonatomic, copy) NSString *pickAccentFg;
@property(nonatomic, strong) ReasonixWeakScriptMessageHandler *messageProxy;
- (void)emitStateLoading:(BOOL)loading;
- (void)applyPickMode;
@end

static NSString *ReasonixPickScript(void) {
    // Prefer tight, text-bearing targets (e.g. Baidu .title-contenta) over large
    // containers; show a Codex/Comet-style ".class \"text\"" hover label.
    return @"(() => {\n"
           @"  const HL_ID = '__reasonix_pick_hl';\n"
           @"  const LB_ID = '__reasonix_pick_lb';\n"
           @"  const STATE = (window.__reasonixPick = window.__reasonixPick || {});\n"
           @"  function isPickChrome(el) {\n"
           @"    return !!(el && (el.id === HL_ID || el.id === LB_ID || el.getAttribute?.('data-reasonix-pick')));\n"
           @"  }\n"
           @"  function ensureHl() {\n"
           @"    let el = document.getElementById(HL_ID);\n"
           @"    if (!el) {\n"
           @"      el = document.createElement('div');\n"
           @"      el.id = HL_ID;\n"
           @"      el.setAttribute('data-reasonix-pick', '1');\n"
           @"      el.style.cssText = 'position:fixed;pointer-events:none;z-index:2147483647;border:1.5px solid "
           @"currentColor;background:transparent;border-radius:2px;display:none;box-sizing:border-box;color:#d97757;';\n"
           @"      (document.documentElement || document.body).appendChild(el);\n"
           @"    }\n"
           @"    return el;\n"
           @"  }\n"
           @"  function ensureLb() {\n"
           @"    let el = document.getElementById(LB_ID);\n"
           @"    if (!el) {\n"
           @"      el = document.createElement('div');\n"
           @"      el.id = LB_ID;\n"
           @"      el.setAttribute('data-reasonix-pick', '1');\n"
           @"      el.style.cssText = 'position:fixed;pointer-events:none;z-index:2147483647;display:none;"
           @"max-width:min(420px,calc(100vw - 16px));padding:3px 8px;border-radius:4px;background:#d97757;color:#fff;"
           @"font:12px/1.35 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:nowrap;"
           @"overflow:hidden;text-overflow:ellipsis;box-shadow:0 4px 14px rgba(15,23,42,0.18);';\n"
           @"      (document.documentElement || document.body).appendChild(el);\n"
           @"    }\n"
           @"    return el;\n"
           @"  }\n"
           @"  function shortClass(el) {\n"
           @"    if (!(el instanceof Element)) return '';\n"
           @"    if (el.id) return '#' + CSS.escape(el.id);\n"
           @"    const raw = typeof el.className === 'string' ? el.className : (el.className?.baseVal || '');\n"
           @"    const cls = raw.trim().split(/\\s+/).filter(Boolean)[0];\n"
           @"    if (cls) return '.' + cls;\n"
           @"    return (el.tagName || '').toLowerCase();\n"
           @"  }\n"
           @"  function cssPath(el) {\n"
           @"    if (!(el instanceof Element)) return '';\n"
           @"    if (el.id) return '#' + CSS.escape(el.id);\n"
           @"    const parts = [];\n"
           @"    let cur = el;\n"
           @"    while (cur && cur.nodeType === 1 && parts.length < 6) {\n"
           @"      const tag = cur.tagName.toLowerCase();\n"
           @"      if (tag === 'html' || tag === 'body') { parts.unshift(tag); break; }\n"
           @"      const parent = cur.parentElement;\n"
           @"      if (!parent) { parts.unshift(tag); break; }\n"
           @"      const siblings = Array.from(parent.children).filter((c) => c.tagName === cur.tagName);\n"
           @"      if (siblings.length === 1) parts.unshift(tag);\n"
           @"      else parts.unshift(tag + ':nth-of-type(' + (siblings.indexOf(cur) + 1) + ')');\n"
           @"      cur = parent;\n"
           @"    }\n"
           @"    return parts.join(' > ');\n"
           @"  }\n"
           @"  function elementText(el) {\n"
           @"    return ((el.innerText || el.textContent || el.getAttribute?.('aria-label') || el.value || '') + '')\n"
           @"      .replace(/\\s+/g, ' ').trim();\n"
           @"  }\n"
           @"  function scoreTarget(el) {\n"
           @"    const r = el.getBoundingClientRect();\n"
           @"    if (r.width < 4 || r.height < 4) return -1e9;\n"
           @"    const area = r.width * r.height;\n"
           @"    const vw = Math.max(1, window.innerWidth);\n"
           @"    const vh = Math.max(1, window.innerHeight);\n"
           @"    if (area > vw * vh * 0.35) return -1e9;\n"
           @"    if (r.width > vw * 0.96 && r.height > 100) return -1e9;\n"
           @"    const text = elementText(el);\n"
           @"    let score = 0;\n"
           @"    if (text.length >= 2 && text.length <= 140) score += 55 - Math.min(35, Math.abs(text.length - 28) * 0.6);\n"
           @"    else if (text.length > 140) score -= 20;\n"
           @"    if (el.matches?.('a,button,summary,[role=\"button\"],input,textarea,select')) score += 28;\n"
           @"    const cls = typeof el.className === 'string' ? el.className : (el.className?.baseVal || '');\n"
           @"    if (/title|content|text|link|item|name|label|headline/i.test(cls)) score += 30;\n"
           @"    // Prefer tighter boxes (reference: hot-search title span, not whole row).\n"
           @"    score -= Math.log2(area + 1) * 2.4;\n"
           @"    if (r.height > 96) score -= 12;\n"
           @"    if (el.children && el.children.length > 8) score -= 10;\n"
           @"    return score;\n"
           @"  }\n"
           @"  function resolveTarget(raw) {\n"
           @"    if (!raw || isPickChrome(raw)) return null;\n"
           @"    if (raw === document.documentElement || raw === document.body) return null;\n"
           @"    const candidates = [];\n"
           @"    let cur = raw;\n"
           @"    for (let i = 0; i < 8 && cur && cur !== document.body && cur !== document.documentElement; i++) {\n"
           @"      if (!isPickChrome(cur)) candidates.push(cur);\n"
           @"      cur = cur.parentElement;\n"
           @"    }\n"
           @"    let best = null;\n"
           @"    let bestScore = -1e9;\n"
           @"    for (const el of candidates) {\n"
           @"      const s = scoreTarget(el);\n"
           @"      if (s > bestScore) { bestScore = s; best = el; }\n"
           @"    }\n"
           @"    return best || raw;\n"
           @"  }\n"
           @"  function placeHl(el) {\n"
           @"    const hl = ensureHl();\n"
           @"    const lb = ensureLb();\n"
           @"    const accent = STATE.accent || '#d97757';\n"
           @"    const accentFg = STATE.accentFg || '#ffffff';\n"
           @"    hl.style.borderColor = accent;\n"
           @"    hl.style.background = 'color-mix(in srgb, ' + accent + ' 14%, transparent)';\n"
           @"    lb.style.background = accent;\n"
           @"    lb.style.color = accentFg;\n"
           @"    if (!el) { hl.style.display = 'none'; lb.style.display = 'none'; return; }\n"
           @"    // Selected + describe popup: hide tip and in-page box.\n"
           @"    if (STATE.locked) {\n"
           @"      hl.style.display = 'none';\n"
           @"      lb.style.display = 'none';\n"
           @"      try { lb.remove(); } catch (_) {}\n"
           @"      return;\n"
           @"    }\n"
           @"    const r = el.getBoundingClientRect();\n"
           @"    if (r.width < 2 || r.height < 2) { hl.style.display = 'none'; lb.style.display = 'none'; return; }\n"
           @"    hl.style.display = 'block';\n"
           @"    hl.style.left = Math.max(0, r.left) + 'px';\n"
           @"    hl.style.top = Math.max(0, r.top) + 'px';\n"
           @"    hl.style.width = Math.max(2, r.width) + 'px';\n"
           @"    hl.style.height = Math.max(2, r.height) + 'px';\n"
           @"    const text = elementText(el).slice(0, 80);\n"
           @"    lb.textContent = shortClass(el) + (text ? (' \"' + text.replace(/\"/g, \"'\") + '\"') : '');\n"
           @"    lb.style.display = 'block';\n"
           @"    const lbW = Math.min(420, lb.offsetWidth || 160);\n"
           @"    let lx = Math.max(8, Math.min(r.left, window.innerWidth - lbW - 8));\n"
           @"    let ly = r.bottom + 6;\n"
           @"    if (ly + 24 > window.innerHeight) ly = Math.max(8, r.top - 28);\n"
           @"    lb.style.left = lx + 'px';\n"
           @"    lb.style.top = ly + 'px';\n"
           @"  }\n"
           @"  function onMove(e) {\n"
           @"    if (!STATE.enabled || STATE.locked) return;\n"
           @"    const raw = document.elementFromPoint(e.clientX, e.clientY);\n"
           @"    placeHl(resolveTarget(raw));\n"
           @"  }\n"
           @"  function onClick(e) {\n"
           @"    if (!STATE.enabled || STATE.locked) return;\n"
           @"    e.preventDefault();\n"
           @"    e.stopPropagation();\n"
           @"    if (typeof e.stopImmediatePropagation === 'function') e.stopImmediatePropagation();\n"
           @"    const raw = document.elementFromPoint(e.clientX, e.clientY);\n"
           @"    const el = resolveTarget(raw);\n"
           @"    if (!el) return;\n"
           @"    const r = el.getBoundingClientRect();\n"
           @"    const text = elementText(el).slice(0, 500);\n"
           @"    STATE.locked = true;\n"
           @"    placeHl(el);\n"
           @"    try {\n"
           @"      window.webkit.messageHandlers.reasonixEmbed.postMessage({\n"
           @"        type: 'pick',\n"
           @"        x: r.left, y: r.top, width: r.width, height: r.height,\n"
           @"        selector: cssPath(el), tagName: el.tagName || '', text: text\n"
           @"      });\n"
           @"    } catch (_) {}\n"
           @"  }\n"
           @"  function onKey(e) {\n"
           @"    if (!STATE.enabled) return;\n"
           @"    if (e.key === 'Escape') {\n"
           @"      e.preventDefault();\n"
           @"      try { window.webkit.messageHandlers.reasonixEmbed.postMessage({ type: 'cancel' }); } catch (_) {}\n"
           @"    }\n"
           @"  }\n"
           @"  STATE.enable = function() {\n"
           @"    STATE.enabled = true;\n"
           @"    STATE.locked = false;\n"
           @"    document.addEventListener('mousemove', onMove, true);\n"
           @"    document.addEventListener('click', onClick, true);\n"
           @"    document.addEventListener('keydown', onKey, true);\n"
           @"    document.documentElement.style.cursor = 'crosshair';\n"
           @"  };\n"
           @"  STATE.disable = function() {\n"
           @"    STATE.enabled = false;\n"
           @"    STATE.locked = false;\n"
           @"    document.removeEventListener('mousemove', onMove, true);\n"
           @"    document.removeEventListener('click', onClick, true);\n"
           @"    document.removeEventListener('keydown', onKey, true);\n"
           @"    document.documentElement.style.cursor = '';\n"
           @"    const hl = document.getElementById(HL_ID);\n"
           @"    if (hl) hl.remove();\n"
           @"    const lb = document.getElementById(LB_ID);\n"
           @"    if (lb) lb.remove();\n"
           @"  };\n"
           @"  STATE.enable();\n"
           @"})();";
}

@implementation ReasonixEmbedBrowserController

- (void)emitStateLoading:(BOOL)loading {
    WKWebView *wv = self.webView;
    if (wv == nil) {
        return;
    }
    NSString *url = wv.URL.absoluteString ?: @"";
    NSString *title = wv.title ?: @"";
    ReasonixEmbedBrowserEmitState(url.UTF8String, title.UTF8String, wv.canGoBack ? 1 : 0, wv.canGoForward ? 1 : 0,
                                  loading ? 1 : 0);
}

- (void)applyPickMode {
    WKWebView *wv = self.webView;
    if (wv == nil) {
        return;
    }
    if (self.pickMode) {
        NSString *accent = self.pickAccent.length > 0 ? self.pickAccent : @"#d97757";
        NSString *accentFg = self.pickAccentFg.length > 0 ? self.pickAccentFg : @"#ffffff";
        NSString * (^quote)(NSString *) = ^NSString *(NSString *raw) {
            NSMutableString *out = [NSMutableString stringWithString:@"\""];
            for (NSUInteger i = 0; i < raw.length; i++) {
                unichar c = [raw characterAtIndex:i];
                if (c == '\\' || c == '"') {
                    [out appendFormat:@"\\%C", c];
                } else if (c == '\n') {
                    [out appendString:@"\\n"];
                } else {
                    [out appendFormat:@"%C", c];
                }
            }
            [out appendString:@"\""];
            return out;
        };
        // One evaluate call so theme colors are applied before pick script boots.
        NSString *script = [NSString stringWithFormat:
            @"window.__reasonixPick=window.__reasonixPick||{};"
             "window.__reasonixPick.accent=%@;"
             "window.__reasonixPick.accentFg=%@;"
             "%@",
            quote(accent), quote(accentFg), ReasonixPickScript()];
        [wv evaluateJavaScript:script completionHandler:nil];
    } else {
        [wv evaluateJavaScript:@"try{window.__reasonixPick&&window.__reasonixPick.disable()}catch(e){}"
             completionHandler:nil];
    }
}

- (void)webView:(WKWebView *)webView didStartProvisionalNavigation:(WKNavigation *)navigation {
    [self emitStateLoading:YES];
}

- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
    [self emitStateLoading:NO];
    if (self.pickMode) {
        [self applyPickMode];
    }
}

- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)navigation withError:(NSError *)error {
    [self emitStateLoading:NO];
    if (error != nil && error.code != NSURLErrorCancelled) {
        ReasonixEmbedBrowserEmitError(error.localizedDescription.UTF8String);
    }
}

- (void)webView:(WKWebView *)webView didFailProvisionalNavigation:(WKNavigation *)navigation withError:(NSError *)error {
    [self emitStateLoading:NO];
    if (error != nil && error.code != NSURLErrorCancelled) {
        ReasonixEmbedBrowserEmitError(error.localizedDescription.UTF8String);
    }
}

- (void)webView:(WKWebView *)webView
    decidePolicyForNavigationAction:(WKNavigationAction *)navigationAction
                    decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    NSURL *url = navigationAction.request.URL;
    NSString *scheme = url.scheme.lowercaseString;
    BOOL isMainFrame = navigationAction.targetFrame == nil || navigationAction.targetFrame.isMainFrame;
    if ([scheme isEqualToString:@"http"] || [scheme isEqualToString:@"https"] || [scheme isEqualToString:@"about"] ||
        [scheme isEqualToString:@"blob"]) {
        decisionHandler(WKNavigationActionPolicyAllow);
        return;
    }
    if ([scheme isEqualToString:@"data"] && !isMainFrame) {
        decisionHandler(WKNavigationActionPolicyAllow);
        return;
    }
    decisionHandler(WKNavigationActionPolicyCancel);
}

- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    if (![message.name isEqualToString:@"reasonixEmbed"]) {
        return;
    }
    id body = message.body;
    if (![body isKindOfClass:[NSDictionary class]]) {
        return;
    }
    NSDictionary *dict = (NSDictionary *)body;
    NSString *type = [dict[@"type"] isKindOfClass:[NSString class]] ? dict[@"type"] : @"";
    if ([type isEqualToString:@"cancel"]) {
        // Frontend owns leaving pick mode; just clear the in-page lock/highlight.
        self.pickMode = NO;
        [self applyPickMode];
        return;
    }
    if (![type isEqualToString:@"pick"]) {
        return;
    }
    double x = [dict[@"x"] respondsToSelector:@selector(doubleValue)] ? [dict[@"x"] doubleValue] : 0;
    double y = [dict[@"y"] respondsToSelector:@selector(doubleValue)] ? [dict[@"y"] doubleValue] : 0;
    double w = [dict[@"width"] respondsToSelector:@selector(doubleValue)] ? [dict[@"width"] doubleValue] : 0;
    double h = [dict[@"height"] respondsToSelector:@selector(doubleValue)] ? [dict[@"height"] doubleValue] : 0;
    NSString *selector = [dict[@"selector"] isKindOfClass:[NSString class]] ? dict[@"selector"] : @"";
    NSString *tagName = [dict[@"tagName"] isKindOfClass:[NSString class]] ? dict[@"tagName"] : @"";
    NSString *text = [dict[@"text"] isKindOfClass:[NSString class]] ? dict[@"text"] : @"";
    ReasonixEmbedBrowserEmitPick(x, y, w, h, selector.UTF8String ?: "", tagName.UTF8String ?: "", text.UTF8String ?: "");
}

@end

static ReasonixEmbedBrowserController *gEmbed = nil;

// Always hop to the next main-queue turn — even when already on the main
// thread — so Go→C bindings never run WebKit work on the Wails call stack.
static void reasonix_run_on_main_async(void (^block)(void)) {
    if (block == nil) {
        return;
    }
    dispatch_async(dispatch_get_main_queue(), block);
}

static NSWindow *ReasonixMainWindow(void) {
    id delegate = [NSApp delegate];
    if (delegate != nil) {
        @try {
            id win = [delegate valueForKey:@"mainWindow"];
            if ([win isKindOfClass:[NSWindow class]]) {
                return (NSWindow *)win;
            }
        } @catch (__unused NSException *ex) {
        }
    }
    for (NSWindow *window in [NSApp windows]) {
        if (window.isVisible && window.contentView != nil) {
            return window;
        }
    }
    return nil;
}

static WKWebView *ReasonixFindWailsWebViewInView(NSView *view, NSView *skip) {
    if (view == nil || view == skip) {
        return nil;
    }
    if ([view isKindOfClass:[WKWebView class]]) {
        return (WKWebView *)view;
    }
    for (NSView *child in view.subviews) {
        WKWebView *found = ReasonixFindWailsWebViewInView(child, skip);
        if (found != nil) {
            return found;
        }
    }
    return nil;
}

// CSS getBoundingClientRect is relative to the Wails WKWebView, not the window
// contentView (titlebar / fullSizeContentView offsets differ). Map through the
// Wails view so the embed never covers the React toolbar.
static WKWebView *ReasonixWailsWebView(void) {
    NSWindow *window = ReasonixMainWindow();
    if (window == nil) {
        return nil;
    }
    return ReasonixFindWailsWebViewInView(window.contentView, gEmbed.webView);
}

static char *ReasonixDupString(NSString *message) {
    if (message == nil || message.length == 0) {
        return NULL;
    }
    const char *utf8 = message.UTF8String;
    if (utf8 == NULL) {
        return NULL;
    }
    return strdup(utf8);
}

static NSRect ReasonixClientRectInParent(NSView *wailsView, NSView *parent, double x, double y, double w, double h) {
    if (w < 1) {
        w = 1;
    }
    if (h < 1) {
        h = 1;
    }
    CGFloat originY = (CGFloat)y;
    if (wailsView != nil && ![wailsView isFlipped]) {
        originY = wailsView.bounds.size.height - (CGFloat)y - (CGFloat)h;
    } else if (wailsView == nil && parent != nil && ![parent isFlipped]) {
        originY = parent.bounds.size.height - (CGFloat)y - (CGFloat)h;
    }
    NSRect inWails = NSMakeRect((CGFloat)x, originY, (CGFloat)w, (CGFloat)h);
    if (wailsView == nil || parent == nil || wailsView == parent) {
        return inWails;
    }
    return [wailsView convertRect:inWails toView:parent];
}

static void ReasonixAttachEmbedAboveWails(WKWebView *embed) {
    if (embed == nil) {
        return;
    }
    WKWebView *wails = ReasonixWailsWebView();
    NSWindow *window = ReasonixMainWindow();
    NSView *parent = wails.superview ?: window.contentView;
    if (parent == nil) {
        return;
    }
    if (embed.superview != parent) {
        [embed removeFromSuperview];
        if (wails != nil && wails.superview == parent) {
            [parent addSubview:embed positioned:NSWindowAbove relativeTo:wails];
        } else {
            [parent addSubview:embed positioned:NSWindowAbove relativeTo:nil];
        }
    } else if (wails != nil) {
        [parent addSubview:embed positioned:NSWindowAbove relativeTo:wails];
    }
}

static void ReasonixEnsureEmbedOnMain(void (^then)(NSError *error)) {
    if (gEmbed.webView != nil) {
        if (then) {
            then(nil);
        }
        return;
    }
    NSWindow *window = ReasonixMainWindow();
    if (window == nil || window.contentView == nil) {
        if (then) {
            then([NSError errorWithDomain:@"reasonix.embed" code:1 userInfo:@{
                NSLocalizedDescriptionKey : @"找不到主窗口，无法创建内嵌浏览器"
            }]);
        }
        return;
    }
    WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
    config.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
    if (@available(macOS 11.0, *)) {
        config.defaultWebpagePreferences.allowsContentJavaScript = YES;
    }

    ReasonixEmbedBrowserController *controller = [[ReasonixEmbedBrowserController alloc] init];
    ReasonixWeakScriptMessageHandler *proxy = [[ReasonixWeakScriptMessageHandler alloc] init];
    proxy.target = controller;
    controller.messageProxy = proxy;
    [config.userContentController addScriptMessageHandler:proxy name:@"reasonixEmbed"];

    WKWebView *webView = [[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 100, 100) configuration:config];
    webView.hidden = YES;
    webView.autoresizingMask = NSViewNotSizable;
#if DEBUG
    if (@available(macOS 13.3, *)) {
        webView.inspectable = YES;
    }
#endif

    controller.webView = webView;
    controller.visible = NO;
    controller.pickMode = NO;
    webView.navigationDelegate = controller;

    // Assign gEmbed before attach so Wails lookup can skip our view.
    gEmbed = controller;
    ReasonixAttachEmbedAboveWails(webView);
    if (then) {
        then(nil);
    }
}

int ReasonixEmbedAvailable(void) { return 1; }

const char *ReasonixEmbedEngineName(void) { return "webkit"; }

void ReasonixEmbedShow(void) {
    reasonix_run_on_main_async(^{
        ReasonixEnsureEmbedOnMain(^(NSError *error) {
            if (error != nil) {
                ReasonixEmbedBrowserEmitError(error.localizedDescription.UTF8String);
                return;
            }
            if (gEmbed.webView == nil) {
                return;
            }
            gEmbed.visible = YES;
            gEmbed.webView.hidden = NO;
            ReasonixAttachEmbedAboveWails(gEmbed.webView);
            // Refresh nav chrome without forcing a reload.
            [gEmbed emitStateLoading:gEmbed.webView.isLoading];
        });
    });
}

void ReasonixEmbedHide(void) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView == nil) {
            return;
        }
        gEmbed.visible = NO;
        gEmbed.webView.hidden = YES;
    });
}

void ReasonixEmbedDestroy(void) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView != nil) {
            gEmbed.pickMode = NO;
            [gEmbed.webView.configuration.userContentController removeScriptMessageHandlerForName:@"reasonixEmbed"];
            gEmbed.webView.navigationDelegate = nil;
            [gEmbed.webView stopLoading];
            [gEmbed.webView removeFromSuperview];
            gEmbed.webView = nil;
        }
        gEmbed = nil;
    });
}

void ReasonixEmbedSetBounds(double x, double y, double w, double h) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView == nil) {
            return;
        }
        // Ignore degenerate frames that would cover the whole window / toolbar.
        if (w < 32 || h < 32) {
            return;
        }
        ReasonixAttachEmbedAboveWails(gEmbed.webView);
        NSView *parent = gEmbed.webView.superview;
        if (parent == nil) {
            return;
        }
        WKWebView *wails = ReasonixWailsWebView();
        gEmbed.webView.frame = ReasonixClientRectInParent(wails, parent, x, y, w, h);
        // Visibility is owned exclusively by Show/Hide — never unhide here.
        gEmbed.webView.hidden = !gEmbed.visible;
    });
}

void ReasonixEmbedNavigate(const char *url) {
    if (url == NULL || url[0] == '\0') {
        return;
    }
    NSString *urlString = [NSString stringWithUTF8String:url];
    NSURL *nsURL = [NSURL URLWithString:urlString];
    NSString *scheme = nsURL.scheme.lowercaseString;
    if (nsURL == nil || !([scheme isEqualToString:@"http"] || [scheme isEqualToString:@"https"])) {
        ReasonixEmbedBrowserEmitError("仅允许 http(s) URL");
        return;
    }
    reasonix_run_on_main_async(^{
        ReasonixEnsureEmbedOnMain(^(NSError *error) {
            if (error != nil) {
                ReasonixEmbedBrowserEmitError(error.localizedDescription.UTF8String);
                return;
            }
            // Do not touch visibility — frontend Show/Hide is the sole owner.
            gEmbed.webView.hidden = !gEmbed.visible;
            NSURLRequest *request = [NSURLRequest requestWithURL:nsURL];
            [gEmbed.webView loadRequest:request];
            [gEmbed emitStateLoading:YES];
        });
    });
}

void ReasonixEmbedReload(void) {
    reasonix_run_on_main_async(^{
        [gEmbed.webView reload];
    });
}

void ReasonixEmbedGoBack(void) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView.canGoBack) {
            [gEmbed.webView goBack];
        }
    });
}

void ReasonixEmbedGoForward(void) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView.canGoForward) {
            [gEmbed.webView goForward];
        }
    });
}

void ReasonixEmbedSetZoom(double factor) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView == nil) {
            return;
        }
        if (@available(macOS 11.0, *)) {
            gEmbed.webView.pageZoom = factor;
        } else {
            gEmbed.webView.magnification = factor;
        }
    });
}

void ReasonixEmbedSetPickMode(int enabled, const char *accent, const char *accentFg) {
    NSString *accentStr = accent ? [NSString stringWithUTF8String:accent] : @"";
    NSString *accentFgStr = accentFg ? [NSString stringWithUTF8String:accentFg] : @"";
    reasonix_run_on_main_async(^{
        ReasonixEnsureEmbedOnMain(^(NSError *error) {
            if (error != nil) {
                ReasonixEmbedBrowserEmitError(error.localizedDescription.UTF8String);
                return;
            }
            gEmbed.pickMode = enabled != 0;
            if (accentStr.length > 0) {
                gEmbed.pickAccent = accentStr;
            }
            if (accentFgStr.length > 0) {
                gEmbed.pickAccentFg = accentFgStr;
            }
            [gEmbed applyPickMode];
        });
    });
}

void ReasonixEmbedSnapshotPNGAsync(void) {
    reasonix_run_on_main_async(^{
        if (gEmbed.webView == nil) {
            ReasonixEmbedBrowserSnapshotDone("");
            return;
        }
        [gEmbed.webView takeSnapshotWithConfiguration:nil
                                    completionHandler:^(NSImage *snapshot, NSError *error) {
                                      if (error != nil || snapshot == nil) {
                                          NSString *msg = error.localizedDescription ?: @"截图失败";
                                          char *dup = ReasonixDupString([@"error:" stringByAppendingString:msg]);
                                          ReasonixEmbedBrowserSnapshotDone(dup ?: "error:截图失败");
                                          free(dup);
                                          return;
                                      }
                                      NSData *tiff = [snapshot TIFFRepresentation];
                                      NSBitmapImageRep *rep = tiff ? [[NSBitmapImageRep alloc] initWithData:tiff] : nil;
                                      NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                                      if (png == nil || png.length == 0) {
                                          ReasonixEmbedBrowserSnapshotDone("error:截图为空");
                                          return;
                                      }
                                      NSString *b64 = [png base64EncodedStringWithOptions:0];
                                      NSString *dataURL = [@"data:image/png;base64," stringByAppendingString:b64];
                                      char *dup = ReasonixDupString(dataURL);
                                      ReasonixEmbedBrowserSnapshotDone(dup ?: "");
                                      free(dup);
                                    }];
    });
}
