#ifdef REASONIX_TRANSCRIPT_SMOKE

#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <WebKit/WebKit.h>
#include <string.h>
#include <unistd.h>

@interface ReasonixTranscriptSmokeHost : NSObject <WKNavigationDelegate, WKScriptMessageHandler>
@property(nonatomic, strong) WKWebView *webView;
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, copy) NSString *script;
@property(nonatomic, copy) NSString *result;
@property(nonatomic, strong) NSTimer *wheelTimer;
@property(nonatomic) NSInteger wheelTick;
@property(nonatomic) NSInteger finishWheelEvents;
@property(nonatomic) NSInteger finishTailStableChecks;
@property(nonatomic) NSPoint transcriptPoint;
@property(nonatomic) BOOL directWheelFallback;
@property(nonatomic) BOOL loaded;
@property(nonatomic) BOOL ready;
@property(nonatomic) BOOL done;
@end

@implementation ReasonixTranscriptSmokeHost

- (void)ensureInteractionFocus {
  if (NSApp.isActive && self.window.isKeyWindow) return;
  [self.window orderFrontRegardless];
  [self.window makeMainWindow];
  [self.window makeKeyWindow];
  [self.window makeFirstResponder:self.webView];
  // This executable is a native-input test host, not the product process.
  // Reclaiming focus keeps a long wheel workload deterministic if another
  // runner window becomes active between its ready and finish boundaries.
  [NSApp activateIgnoringOtherApps:YES];
}

- (void)finishWithResult:(NSString *)result {
  if (self.done) return;
  self.done = YES;
  self.result = result;
  [self.wheelTimer invalidate];
  [NSApp stop:nil];
  NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                      location:NSZeroPoint
                                 modifierFlags:0
                                     timestamp:0
                                  windowNumber:0
                                       context:nil
                                       subtype:0
                                         data1:0
                                         data2:0];
  [NSApp postEvent:wake atStart:NO];
}

- (void)finishTimeoutWithPrefix:(NSString *)prefix {
  if (self.done) return;
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
    NSDictionary *payload = @{
      @"type": @"error",
      @"message": [NSString stringWithFormat:
        @"%@; diagnostic probe timed out (ready=%d wheel=%ld/1200 finish=%ld/240 timer=%d key=%d main=%d)",
        prefix, self.ready, (long)self.wheelTick, (long)self.finishWheelEvents,
        self.wheelTimer != nil, self.window.isKeyWindow, self.window.isMainWindow],
    };
    NSData *data = [NSJSONSerialization dataWithJSONObject:payload options:0 error:nil];
    NSString *fallback = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
    [self finishWithResult:fallback];
  });
  NSString *probe = @"JSON.stringify({readyState:document.readyState,topicCount:document.querySelectorAll('.project-tree__topic-main').length,hasActiveTopic:Boolean(document.querySelector('.project-tree__topic--active')),transcript:Boolean(document.querySelector('.transcript')),rows:document.querySelector('.transcript')?.dataset.transcriptRowCount||'0',bootstrap:document.querySelector('.transcript')?.dataset.transcriptGeometryBootstrap||'',rect:document.querySelector('.transcript')&&[document.querySelector('.transcript').clientWidth,document.querySelector('.transcript').clientHeight],raf:window.__reasonixSmokeRaf,contract:Boolean(window.__reasonixNativeTranscriptSmoke),phase:window.__reasonixNativeTranscriptSmokeState?.phase||'missing'})";
  [self.webView evaluateJavaScript:probe completionHandler:^(id value, NSError *error) {
    if (self.done) return;
    NSString *detail = error ? error.localizedDescription : [NSString stringWithFormat:@"%@ nativeVisible=%d occlusion=%lu key=%d main=%d", [value description], self.window.isVisible, (unsigned long)self.window.occlusionState, self.window.isKeyWindow, self.window.isMainWindow];
    NSDictionary *payload = @{ @"type": @"error", @"message": [NSString stringWithFormat:@"%@: %@", prefix, detail] };
    NSData *data = [NSJSONSerialization dataWithJSONObject:payload options:0 error:nil];
    [self finishWithResult:[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding]];
  }];
}

- (void)scheduleStartupWatchdog {
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 90 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
    if (self.done || self.ready) return;
    NSString *prefix = self.loaded
      ? @"WKWebView fixture loaded but did not become ready"
      : @"WKWebView navigation timed out";
    [self finishTimeoutWithPrefix:prefix];
  });
}

- (void)scheduleInteractionWatchdog {
  // The deterministic native workload is about 62 seconds at ideal timer
  // cadence (1200 * 40ms, a 2s drain, and the bounded 240 * 50ms tail phase).
  // Hosted WKWebView runners can deliver those timers at roughly half speed,
  // so keep the behavioral workload intact and budget for scheduler variance.
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 150 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
    if (self.done) return;
    [self finishTimeoutWithPrefix:@"WKWebView native interaction timed out after ready"];
  });
}

- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
  (void)navigation;
  self.loaded = YES;
  NSString *injection = [self.script stringByAppendingString:@"\nwindow.__reasonixSmokeRaf=0;requestAnimationFrame(()=>{window.__reasonixSmokeRaf=1;});"];
  [webView evaluateJavaScript:injection completionHandler:^(id value, NSError *error) {
    (void)value;
    if (error) {
      NSString *message = [NSString stringWithFormat:@"{\"type\":\"error\",\"message\":\"WKWebView injection failed: %@\"}", error.localizedDescription];
      [self finishWithResult:message];
    }
  }];
}

- (void)webView:(WKWebView *)webView didFailProvisionalNavigation:(WKNavigation *)navigation withError:(NSError *)error {
  (void)webView;
  (void)navigation;
  NSString *message = [NSString stringWithFormat:@"{\"type\":\"error\",\"message\":\"WKWebView provisional navigation failed: %@\"}", error.localizedDescription];
  [self finishWithResult:message];
}

- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)navigation withError:(NSError *)error {
  (void)webView;
  (void)navigation;
  NSString *message = [NSString stringWithFormat:@"{\"type\":\"error\",\"message\":\"WKWebView navigation failed: %@\"}", error.localizedDescription];
  [self finishWithResult:message];
}

- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
  (void)controller;
  if (![message.body isKindOfClass:[NSString class]]) return;
  NSString *body = (NSString *)message.body;
  NSData *data = [body dataUsingEncoding:NSUTF8StringEncoding];
  NSDictionary *payload = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
  NSString *type = [payload isKindOfClass:[NSDictionary class]] ? payload[@"type"] : nil;
  if ([type isEqualToString:@"ready"]) {
    if (self.ready) return;
    self.ready = YES;
    [self scheduleInteractionWatchdog];
    self.wheelTick = 0;
    NSDictionary *point = [payload[@"point"] isKindOfClass:[NSDictionary class]] ? payload[@"point"] : nil;
    const CGFloat x = [point[@"x"] respondsToSelector:@selector(doubleValue)] ? [point[@"x"] doubleValue] : NSMidX(self.webView.bounds);
    const CGFloat y = [point[@"y"] respondsToSelector:@selector(doubleValue)] ? [point[@"y"] doubleValue] : NSMidY(self.webView.bounds);
    self.transcriptPoint = NSMakePoint(x, NSHeight(self.webView.bounds) - y);
    [self.window makeFirstResponder:self.webView];
    [self ensureInteractionFocus];
    // The contract waits for settled Virtuoso geometry before posting ready.
    // Re-establish native focus at that boundary instead of relying on the
    // workflow's earlier best-effort app activation.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 200 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
      if (self.done) return;
      // WKWebView processes native wheel input asynchronously. A 24ms source
      // cadence can fill the hosted runner's WebContent event queue before its
      // geometry probe runs; 40ms preserves the full 1200-event workload while
      // keeping native input and painted frames in bounded lockstep.
      self.wheelTimer = [NSTimer scheduledTimerWithTimeInterval:0.040
                                                         target:self
                                                       selector:@selector(sendWheel:)
                                                       userInfo:nil
                                                        repeats:YES];
    });
    return;
  }
  if ([type isEqualToString:@"result"] || [type isEqualToString:@"error"]) {
    [self finishWithResult:body];
  }
}

- (void)dispatchWheelDelta:(int32_t)delta atViewPoint:(NSPoint)viewPoint {
  [self ensureInteractionFocus];
  // Hosted macOS 26 can deliver a directly invoked scrollWheel: event into
  // WebContent without running its default scrolling action. Queue a native
  // mouse-wheel event for this process instead, so AppKit and WebKit own the
  // complete responder/default-action path. Line units avoid the hosted
  // runner's 10:1 pixel-to-line reinterpretation while still exercising a
  // public, platform-native wheel source.
  int32_t wheelDelta = delta;
  CGScrollEventUnit unit = kCGScrollEventUnitPixel;
  if (!self.directWheelFallback) {
    wheelDelta = delta / 10;
    if (wheelDelta == 0 && delta != 0) wheelDelta = delta < 0 ? -1 : 1;
    unit = kCGScrollEventUnitLine;
  }
  CGEventRef cgEvent = CGEventCreateScrollWheelEvent(NULL, unit, 1, wheelDelta);
  if (cgEvent == NULL) return;
  NSPoint windowPoint = [self.webView convertPoint:viewPoint toView:nil];
  NSPoint screenPoint = [self.window convertPointToScreen:windowPoint];
  NSScreen *screen = self.window.screen ?: NSScreen.mainScreen;
  NSRect screenFrame = screen.frame;
  CGPoint quartzPoint = CGPointMake(screenPoint.x, NSMaxY(screenFrame) - screenPoint.y);
  CGEventSetLocation(cgEvent, quartzPoint);
  CGEventSetIntegerValueField(
    cgEvent,
    kCGMouseEventWindowUnderMousePointer,
    self.window.windowNumber
  );
  CGEventSetIntegerValueField(
    cgEvent,
    kCGMouseEventWindowUnderMousePointerThatCanHandleThisEvent,
    self.window.windowNumber
  );
  if (self.directWheelFallback) {
    NSEvent *event = [NSEvent eventWithCGEvent:cgEvent];
    NSView *target = [self.webView hitTest:viewPoint] ?: self.webView;
    if (event != nil) [target scrollWheel:event];
  } else {
    CGEventPostToPid(getpid(), cgEvent);
  }
  CFRelease(cgEvent);
}

- (void)probeQueuedWheelDelivery {
  if (self.done || self.directWheelFallback) return;
  NSString *probe = @"window.__reasonixNativeTranscriptSmokeState?.wheelEvents||0";
  [self.webView evaluateJavaScript:probe completionHandler:^(id value, NSError *error) {
    if (self.done || self.directWheelFallback || error) return;
    if ([value respondsToSelector:@selector(integerValue)] && [value integerValue] == 0) {
      // Posting to our own native queue needs no WebView test hook, but older
      // local macOS installations can still require an Accessibility grant.
      // A proven zero-delivery result selects the responder route that those
      // hosts accept; hosted macOS 26 stays on the full AppKit queue path.
      self.directWheelFallback = YES;
    }
  }];
}

- (void)dispatchWheelDelta:(int32_t)delta {
  [self dispatchWheelDelta:delta atViewPoint:self.transcriptPoint];
}

- (void)finishNativeWheelTail {
  if (self.done) return;
  NSString *probe = @"Math.max(0,document.querySelector('.transcript').scrollHeight-document.querySelector('.transcript').scrollTop-document.querySelector('.transcript').clientHeight)";
  [self.webView evaluateJavaScript:probe completionHandler:^(id value, NSError *error) {
    if (self.done) return;
    const double distance = !error && [value respondsToSelector:@selector(doubleValue)]
      ? [value doubleValue]
      : INFINITY;
    if (distance <= 4) {
      self.finishTailStableChecks += 1;
      if (self.finishTailStableChecks >= 2) {
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 700 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
          [self.webView evaluateJavaScript:@"window.__reasonixNativeTranscriptSmoke.finish()" completionHandler:nil];
        });
        return;
      }
      dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 200 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
        [self finishNativeWheelTail];
      });
      return;
    }
    self.finishTailStableChecks = 0;
    if (self.finishWheelEvents >= 240) {
      dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 700 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
        [self.webView evaluateJavaScript:@"window.__reasonixNativeTranscriptSmoke.finish()" completionHandler:nil];
      });
      return;
    }
    [self finishWheelBurst:MIN(8, 240 - self.finishWheelEvents)];
  }];
}

- (void)finishWheelBurst:(NSInteger)remaining {
  if (self.done) return;
  if (remaining > 0) {
    [self dispatchWheelDelta:-120];
    self.finishWheelEvents += 1;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
      [self finishWheelBurst:remaining - 1];
    });
    return;
  }
  [self finishNativeWheelTail];
}

- (void)sendWheel:(NSTimer *)timer {
  // Sustain small high-resolution trackpad deltas without outrunning
  // Virtuoso's mount window on a loaded hosted WebView.
  const NSInteger totalTicks = 1200;
  if (self.wheelTick >= totalTicks) {
    [timer invalidate];
    self.wheelTimer = nil;
    // Hosted WebKit can coalesce events while Virtuoso commits its last range.
    // After an idle boundary, keep sending bounded ordinary wheel events until
    // the native extent is stably at the tail. This remains native input; the
    // JavaScript probe only decides when the platform-specific fixture stops.
    // Drain WKWebView's native event queue before asking the WebContent process
    // for geometry. Without this boundary all 1200 events can complete on the
    // host while the first JavaScript tail probe remains queued indefinitely.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
      self.finishWheelEvents = 0;
      self.finishTailStableChecks = 0;
      [self finishWheelBurst:8];
    });
    return;
  }
  [self dispatchWheelDelta:-24];
  self.wheelTick += 1;
  if (self.wheelTick == 50) [self probeQueuedWheelDelivery];
}

@end

char *reasonix_transcript_smoke_darwin(const char *url, const char *script) {
  @autoreleasepool {
    if (![NSThread isMainThread]) {
      return strdup("{\"type\":\"error\",\"message\":\"WKWebView host is not running on the main thread\"}");
    }
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    [NSApp finishLaunching];
    WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
    WKUserContentController *contentController = [[WKUserContentController alloc] init];
    ReasonixTranscriptSmokeHost *host = [[ReasonixTranscriptSmokeHost alloc] init];
    [contentController addScriptMessageHandler:host name:@"reasonixNativeSmoke"];
    configuration.userContentController = contentController;
    host.script = [NSString stringWithUTF8String:script];
    host.webView = [[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 1200, 800) configuration:configuration];
    host.webView.navigationDelegate = host;
    host.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 1200, 800)
                                             styleMask:NSWindowStyleMaskTitled
                                               backing:NSBackingStoreBuffered
                                                 defer:NO];
    host.window.contentView = host.webView;
    host.window.level = CGWindowLevelForKey(kCGScreenSaverWindowLevelKey);
    host.window.canHide = NO;
    host.window.hidesOnDeactivate = NO;
    [host.window center];
    [host.window makeKeyAndOrderFront:nil];
    [host.window orderFrontRegardless];
    [NSApp activate];
    [host.window makeMainWindow];
    [host.window makeKeyWindow];
    [host.webView loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[NSString stringWithUTF8String:url]]]];
    [host scheduleStartupWatchdog];
    [NSApp run];
    [contentController removeScriptMessageHandlerForName:@"reasonixNativeSmoke"];
    [host.window orderOut:nil];
    const char *raw = [host.result ?: @"{\"type\":\"error\",\"message\":\"WKWebView stopped without a result\"}" UTF8String];
    return strdup(raw);
  }
}

#endif
