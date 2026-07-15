//go:build darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static WKWebView *petWebView = nil;
static NSWindow   *petWindow   = nil;

extern void petOnJSEval(const char *result);
extern void petOnSavePos(int x, int y);
extern void petOnSetScale(double scale);
extern void petOnSetSlug(const char *slug);
extern void petOnGetPets(void);
extern void petOnInstallPet(const char *name);
extern void petOnDeletePet(const char *slug);
extern void petOnWebViewReady(void);

void petMoveBy(double dx, double dy);
void petSavePosition(void);

// ── Drag handler ─────────────────────────────────────────────────────────────
@interface PetDragHandler : NSObject <WKScriptMessageHandler>
@end
@implementation PetDragHandler
- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    if (![message.body isKindOfClass:[NSDictionary class]]) return;
    NSDictionary *body = (NSDictionary *)message.body;
    NSString *type = body[@"type"];
    if ([type isEqualToString:@"savePos"]) {
        petSavePosition();
        return;
    }
    petMoveBy([body[@"x"] doubleValue], [body[@"y"] doubleValue]);
}
@end

// ── Command handler (scale, slug, quit, getPets, installPet) ─────────────────
@interface PetCommandHandler : NSObject <WKScriptMessageHandler>
@end
@implementation PetCommandHandler
- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    if (![message.body isKindOfClass:[NSDictionary class]]) return;
    NSDictionary *body = (NSDictionary *)message.body;
    NSString *cmd = body[@"cmd"];
    if ([cmd isEqualToString:@"setScale"]) {
        petOnSetScale([body[@"scale"] doubleValue]);
    } else if ([cmd isEqualToString:@"setSlug"]) {
        petOnSetSlug([body[@"slug"] UTF8String]);
    } else if ([cmd isEqualToString:@"installPet"]) {
        petOnInstallPet([body[@"name"] UTF8String]);
    } else if ([cmd isEqualToString:@"deletePet"]) {
        petOnDeletePet([body[@"slug"] UTF8String]);
    } else if ([cmd isEqualToString:@"openUrl"]) {
        NSString *url = body[@"url"];
        if (url) [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:url]];
    } else if ([cmd isEqualToString:@"getPets"]) {
        petOnGetPets();
    }
}
@end

// ── Navigation delegate: fires petOnWebViewReady after HTML loads ────────────
@interface PetNavDelegate : NSObject <WKNavigationDelegate>
@end
@implementation PetNavDelegate
- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
    petOnWebViewReady();
}
@end

// ── createPetWindow ──────────────────────────────────────────────────────────
void createPetWindow(const char *homeDir, const char *html) {
    (void)homeDir;
    NSString *htmlStr = [NSString stringWithUTF8String:html];
    if (htmlStr == nil) htmlStr = @"<body style=\"background:transparent\"><p>pet</p></body>";

    dispatch_async(dispatch_get_main_queue(), ^{
        // Guard: already created — PetToggle may fire during startup delay.
        if (petWindow != nil) return;
        NSRect frame = NSMakeRect(0, 0, 140, 180);
        petWindow = [[NSWindow alloc] initWithContentRect:frame
                                               styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskTitled
                                                 backing:NSBackingStoreBuffered
                                                   defer:NO];
        [petWindow setOpaque:NO];
        [petWindow setBackgroundColor:[NSColor clearColor]];
        [petWindow setTitlebarAppearsTransparent:YES];
        [petWindow setTitleVisibility:NSWindowTitleHidden];
        [petWindow setMovableByWindowBackground:NO];
        [petWindow setLevel:NSFloatingWindowLevel];
        [petWindow setIgnoresMouseEvents:NO];
        [petWindow setCollectionBehavior:
            NSWindowCollectionBehaviorCanJoinAllSpaces |
            NSWindowCollectionBehaviorFullScreenAuxiliary];
        [petWindow setTitle:@""];

        WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
        WKUserContentController *ctrl = [[WKUserContentController alloc] init];

        NSString *bridgeJS = @"window.__petMoveBy=function(dx,dy){ \
            window.webkit.messageHandlers.petDrag.postMessage({x:dx,y:dy}); \
        }; \
        window.__petSavePos=function(){ \
            window.webkit.messageHandlers.petDrag.postMessage({type:'savePos'}); \
        }; \
        window.__petCommand=function(cmd,args){ \
            window.webkit.messageHandlers.petCommand.postMessage(\
                Object.assign({cmd:cmd},args||{})); \
        };";
        WKUserScript *script = [[WKUserScript alloc] initWithSource:bridgeJS
                                                      injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                                   forMainFrameOnly:YES];
        [ctrl addUserScript:script];

        PetDragHandler *dragHandler = [[PetDragHandler alloc] init];
        [ctrl addScriptMessageHandler:dragHandler name:@"petDrag"];
        PetCommandHandler *cmdHandler = [[PetCommandHandler alloc] init];
        [ctrl addScriptMessageHandler:cmdHandler name:@"petCommand"];
        config.userContentController = ctrl;

        petWebView = [[WKWebView alloc] initWithFrame:frame configuration:config];
        petWebView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        [petWebView setValue:@NO forKey:@"drawsBackground"];
        petWebView.navigationDelegate = [[PetNavDelegate alloc] init];
        for (NSView *subview in [[petWebView subviews] copy]) {
            if ([subview isKindOfClass:[NSScrollView class]]) {
                [(NSScrollView *)subview setHasHorizontalScroller:NO];
                [(NSScrollView *)subview setHasVerticalScroller:NO];
            }
        }
        petWindow.contentView = petWebView;
        [petWebView loadHTMLString:htmlStr baseURL:nil];

        NSScreen *screen = [NSScreen mainScreen];
        NSRect screenFrame = screen.visibleFrame;
        CGFloat x = screenFrame.origin.x + screenFrame.size.width - frame.size.width - 20;
        CGFloat y = screenFrame.origin.y + 20;
        [petWindow setFrameOrigin:NSMakePoint(x, y)];
        [petWindow orderFrontRegardless];
        [petWindow setHidesOnDeactivate:NO];
    });
}

// ── petSetPosition ───────────────────────────────────────────────────────────
void petSetPosition(int x, int y) {
    if (petWindow == nil) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        [petWindow setFrameOrigin:NSMakePoint((CGFloat)x, (CGFloat)y)];
    });
}

// ── petSetWindowScale ────────────────────────────────────────────────────────
void petSetWindowScale(double scale) {
    if (petWindow == nil) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        CGFloat baseW = 140, baseH = 180;
        NSRect frame = [petWindow frame];
        frame.size.width  = baseW * scale;
        frame.size.height = baseH * scale;
        [petWindow setFrame:frame display:YES animate:NO];
    });
}

// ── petSavePosition ──────────────────────────────────────────────────────────
void petSavePosition(void) {
    if (petWindow == nil) return;
    NSRect frame = [petWindow frame];
    petOnSavePos((int)frame.origin.x, (int)frame.origin.y);
}

// ── petEvaluateJS ───────────────────────────────────────────────────────────
void petEvaluateJS(const char *script) {
    if (petWebView == nil || script == NULL) return;
    NSString *js = [NSString stringWithUTF8String:script];
    if (js == nil) return;
    if (![NSThread isMainThread]) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [petWebView evaluateJavaScript:js completionHandler:^(id result, NSError *error) {
                if (error != nil) return;
                if (result != nil && [result isKindOfClass:[NSString class]]) {
                    petOnJSEval([(NSString *)result UTF8String]);
                }
            }];
        });
        return;
    }
    [petWebView evaluateJavaScript:js completionHandler:^(id result, NSError *error) {
        if (error != nil) return;
        if (result != nil && [result isKindOfClass:[NSString class]]) {
            petOnJSEval([(NSString *)result UTF8String]);
        }
    }];
}

// ── petMoveBy ─────────────────────────────────────────────────────────────────
void petMoveBy(double dx, double dy) {
    if (petWindow == nil || (dx == 0 && dy == 0)) return;
    if (![NSThread isMainThread]) {
        dispatch_async(dispatch_get_main_queue(), ^{ petMoveBy(dx, dy); });
        return;
    }
    NSRect frame = [petWindow frame];
    frame.origin.x += dx;
    frame.origin.y -= dy;
    [petWindow setFrameOrigin:frame.origin];
}

// ── petCloseWindow ────────────────────────────────────────────────────────────
void petCloseWindow(void) {
    if (![NSThread isMainThread]) {
        dispatch_async(dispatch_get_main_queue(), ^{ petCloseWindow(); });
        return;
    }
    if (petWebView != nil) {
        [petWebView stopLoading];
        petWebView = nil;
    }
    if (petWindow != nil) {
        [petWindow close];
        petWindow = nil;
    }
}
