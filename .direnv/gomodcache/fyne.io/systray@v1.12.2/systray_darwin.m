//go:build !ios

#import <Cocoa/Cocoa.h>
#include "systray.h"

#if __MAC_OS_X_VERSION_MIN_REQUIRED < 101400

    #ifndef NSControlStateValueOff
      #define NSControlStateValueOff NSOffState
    #endif

    #ifndef NSControlStateValueOn
      #define NSControlStateValueOn NSOnState
    #endif

#endif

@interface MenuItem : NSObject
{
  @public
    NSNumber* menuId;
    NSNumber* parentMenuId;
    NSString* title;
    NSString* tooltip;
    short disabled;
    short checked;
}
-(id) initWithId: (int)theMenuId
withParentMenuId: (int)theParentMenuId
       withTitle: (const char*)theTitle
     withTooltip: (const char*)theTooltip
    withDisabled: (short)theDisabled
     withChecked: (short)theChecked;
     @end
     @implementation MenuItem
     -(id) initWithId: (int)theMenuId
     withParentMenuId: (int)theParentMenuId
            withTitle: (const char*)theTitle
          withTooltip: (const char*)theTooltip
         withDisabled: (short)theDisabled
          withChecked: (short)theChecked
{
  menuId = [NSNumber numberWithInt:theMenuId];
  parentMenuId = [NSNumber numberWithInt:theParentMenuId];
  title = [[NSString alloc] initWithCString:theTitle
                                   encoding:NSUTF8StringEncoding];
  tooltip = [[NSString alloc] initWithCString:theTooltip
                                     encoding:NSUTF8StringEncoding];
  disabled = theDisabled;
  checked = theChecked;
  return self;
}
@end

@interface RightClickDetector : NSView

@property (copy) void (^onRightClicked)(NSEvent *);

@end

@implementation RightClickDetector

- (void)rightMouseUp:(NSEvent *)theEvent {
  if (!self.onRightClicked) {
    return;
  }

  self.onRightClicked(theEvent);
}

@end


@interface SystrayAppDelegate: NSObject <NSApplicationDelegate, NSMenuDelegate>
  - (void) add_or_update_menu_item:(MenuItem*) item;
  - (IBAction)menuHandler:(id)sender;
  - (void)menuWillOpen:(NSMenu*)menu;
  @property (assign) IBOutlet NSWindow *window;
@end

@implementation SystrayAppDelegate
{
  NSStatusItem *statusItem;
  NSMenu *menu;
  NSCondition* cond;
}

@synthesize window = _window;

- (void)applicationDidFinishLaunching:(NSNotification *)aNotification
{
  self->statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];

  self->menu = [[NSMenu alloc] init];
  self->menu.delegate = self;
  self->menu.autoenablesItems = FALSE;
  // Once the user has removed it, the item needs to be explicitly brought back,
  // even restarting the application is insufficient.
  // Since the interface from Go is relatively simple, for now we ensure it's
  // always visible at application startup.
  self->statusItem.visible = TRUE;

  NSStatusBarButton *button = self->statusItem.button;
  button.action = @selector(leftMouseClicked);

  [NSEvent addLocalMonitorForEventsMatchingMask: (NSEventTypeLeftMouseDown|NSEventTypeRightMouseDown)
                                        handler: ^NSEvent *(NSEvent *event) {
    if (event.window != self->statusItem.button.window) {
      return event;
    }

    if (event.modifierFlags & NSEventModifierFlagCommand) {
      return event;
    }

    [self leftMouseClicked];

    return nil;
  }];

  NSSize size = [button frame].size;
  NSRect frame = CGRectMake(0, 0, size.width, size.height);
  RightClickDetector *rightClicker = [[RightClickDetector alloc] initWithFrame:frame];
  rightClicker.onRightClicked = ^(NSEvent *event) {
    [self rightMouseClicked];
  };

  rightClicker.autoresizingMask = (NSViewWidthSizable |
                                   NSViewHeightSizable);
  button.autoresizesSubviews = YES;
  [button addSubview:rightClicker];

  systray_ready();
}

- (void)rightMouseClicked {
  systray_right_click();
}

- (void)leftMouseClicked {
  systray_left_click();
}

- (void)applicationWillTerminate:(NSNotification *)aNotification
{
  systray_on_exit();
}

- (void)setRemovalAllowed {
  NSStatusItemBehavior behavior = [self->statusItem behavior];
  behavior |= NSStatusItemBehaviorRemovalAllowed;
  self->statusItem.behavior = behavior;
}

- (void)setRemovalForbidden {
  NSStatusItemBehavior behavior = [self->statusItem behavior];
  behavior &= ~NSStatusItemBehaviorRemovalAllowed;
  // Ensure the menu item is visible if it was removed, since we're now
  // disallowing removal.
  self->statusItem.visible = TRUE;
  self->statusItem.behavior = behavior;
}

- (void)setIcon:(NSImage *)image {
  statusItem.button.image = image;
  [self updateTitleButtonStyle];
}

- (void)setTitle:(NSString *)title {
  statusItem.button.title = title;
  [self updateTitleButtonStyle];
}

- (void)updateTitleButtonStyle {
  if (statusItem.button.image != nil) {
    if ([statusItem.button.title length] == 0) {
      statusItem.button.imagePosition = NSImageOnly;
    } else {
      statusItem.button.imagePosition = NSImageLeft;
    }
  } else {
    statusItem.button.imagePosition = NSNoImage;
  }
}


- (void)setTooltip:(NSString *)tooltip {
  statusItem.button.toolTip = tooltip;
}

- (IBAction)menuHandler:(id)sender
{
  NSNumber* menuId = [sender representedObject];
  systray_menu_item_selected(menuId.intValue);
}

- (void)menuWillOpen:(NSMenu *)menu {
  systray_menu_will_open();
}

- (void)add_or_update_menu_item:(MenuItem *)item {
  NSMenu *theMenu = self->menu;
  NSMenuItem *parentItem;
  if ([item->parentMenuId integerValue] > 0) {
    parentItem = find_menu_item(menu, item->parentMenuId);
    if (parentItem.hasSubmenu) {
      theMenu = parentItem.submenu;
    } else {
      theMenu = [[NSMenu alloc] init];
      [theMenu setAutoenablesItems:NO];
      [parentItem setSubmenu:theMenu];
    }
  }

  NSMenuItem *menuItem = find_menu_item(theMenu, item->menuId);
  if (menuItem == NULL) {
    menuItem = [theMenu addItemWithTitle:item->title
                               action:@selector(menuHandler:)
                        keyEquivalent:@""];
    [menuItem setRepresentedObject:item->menuId];
  }
  [menuItem setTitle:item->title];
  [menuItem setTag:[item->menuId integerValue]];
  [menuItem setTarget:self];
  [menuItem setToolTip:item->tooltip];
  if (item->disabled == 1) {
    menuItem.enabled = FALSE;
  } else {
    menuItem.enabled = TRUE;
  }
  if (item->checked == 1) {
    menuItem.state = NSControlStateValueOn;
  } else {
    menuItem.state = NSControlStateValueOff;
  }
}

NSMenuItem *find_menu_item(NSMenu *ourMenu, NSNumber *menuId) {
  NSMenuItem *foundItem = [ourMenu itemWithTag:[menuId integerValue]];
  if (foundItem != NULL) {
    return foundItem;
  }
  NSArray *menu_items = ourMenu.itemArray;
  int i;
  for (i = 0; i < [menu_items count]; i++) {
    NSMenuItem *i_item = [menu_items objectAtIndex:i];
    if (i_item.hasSubmenu) {
      foundItem = find_menu_item(i_item.submenu, menuId);
      if (foundItem != NULL) {
        return foundItem;
      }
    }
  }

  return NULL;
};

- (void) add_separator:(NSNumber*) parentMenuId
{
  if (parentMenuId.integerValue != 0) {
    NSMenuItem* menuItem = find_menu_item(menu, parentMenuId);
    if (menuItem != NULL) {
      [menuItem.submenu addItem: [NSMenuItem separatorItem]];
      return;
    }
  }
  [menu addItem: [NSMenuItem separatorItem]];
}

- (void) hide_menu_item:(NSNumber*) menuId
{
  NSMenuItem* menuItem = find_menu_item(menu, menuId);
  if (menuItem != NULL) {
    [menuItem setHidden:TRUE];
  }
}

- (void) setMenuItemIcon:(NSArray*)imageAndMenuId {
  NSImage* image = [imageAndMenuId objectAtIndex:0];
  NSNumber* menuId = [imageAndMenuId objectAtIndex:1];

  NSMenuItem* menuItem;
  menuItem = find_menu_item(menu, menuId);
  if (menuItem == NULL) {
    return;
  }
  menuItem.image = image;
}

- (void)show_menu
{
  // Attach the menu and synthesize a click so AppKit positions it natively,
  // then detach it in menuDidClose: so the next click reaches the button
  // action (and the Go tap handlers) again.
  self->statusItem.menu = self->menu;
  [self->statusItem.button performClick:nil];
}

- (void)menuDidClose:(NSMenu *)menu {
  // Defer the detach so we don't pull the menu out from under AppKit
  // while it is still tearing the menu down.
  dispatch_async(dispatch_get_main_queue(), ^{
    self->statusItem.menu = nil;
  });
}

- (void) show_menu_item:(NSNumber*) menuId
{
  NSMenuItem* menuItem = find_menu_item(menu, menuId);
  if (menuItem != NULL) {
    [menuItem setHidden:FALSE];
  }
}

- (void) remove_menu_item:(NSNumber*) menuId
{
  NSMenuItem* menuItem = find_menu_item(menu, menuId);
  if (menuItem != NULL) {
    [menuItem.menu removeItem:menuItem];
  }
}

- (void) reset_menu
{
  [self->menu removeAllItems];
}

- (void) quit
{
  // This tells the app event loop to stop after processing remaining messages.
  [NSApp stop:self];
  // The event loop won't return until it processes another event.
  // https://stackoverflow.com/a/48064752/149482
  NSPoint eventLocation = NSMakePoint(0, 0);
  NSEvent *customEvent = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                            location:eventLocation
                                       modifierFlags:0
                                           timestamp:0
                                        windowNumber:0
                                             context:nil
                                             subtype:0
                                               data1:0
                                               data2:0];
  [NSApp postEvent:customEvent atStart:NO];
}

@end

bool internalLoop = false;
SystrayAppDelegate *owner;

void setInternalLoop(bool i) {
	internalLoop = i;
}

void registerSystray(void) {
  if (!internalLoop) { // with an external loop we don't take ownership of the app
    return;
  }

  owner = [[SystrayAppDelegate alloc] init];
  [[NSApplication sharedApplication] setDelegate:owner];

  // A workaround to avoid crashing on macOS versions before Catalina. Somehow
  // SIGSEGV would happen inside AppKit if [NSApp run] is called from a
  // different function, even if that function is called right after this.
  if (floor(NSAppKitVersionNumber) <= /*NSAppKitVersionNumber10_14*/ 1671){
    [NSApp run];
  }
}

void nativeEnd(void) {
  systray_on_exit();
}

int nativeLoop(void) {
  if (floor(NSAppKitVersionNumber) > /*NSAppKitVersionNumber10_14*/ 1671){
    [NSApp run];
  }
  return EXIT_SUCCESS;
}

void nativeStart(void) {
  owner = [[SystrayAppDelegate alloc] init];

  NSNotification *launched = [NSNotification notificationWithName:NSApplicationDidFinishLaunchingNotification
                                                        object:[NSApplication sharedApplication]];
  [owner applicationDidFinishLaunching:launched];
}

void runInMainThread(SEL method, id object) {
  [owner
    performSelectorOnMainThread:method
                     withObject:object
                  waitUntilDone: YES];
}

void setIcon(const char* iconBytes, int length, bool template) {
  NSData* buffer = [NSData dataWithBytes: iconBytes length:length];
  @autoreleasepool {
    NSImage *image = [[NSImage alloc] initWithData:buffer];
    [image setSize:NSMakeSize(16, 16)];
    image.template = template;
    runInMainThread(@selector(setIcon:), (id)image);
  }
}

void setMenuItemIcon(const char* iconBytes, int length, int menuId, bool template) {
  NSData* buffer = [NSData dataWithBytes: iconBytes length:length];
  @autoreleasepool {
    NSImage *image = [[NSImage alloc] initWithData:buffer];
    [image setSize:NSMakeSize(16, 16)];
    image.template = template;
    NSNumber *mId = [NSNumber numberWithInt:menuId];
    runInMainThread(@selector(setMenuItemIcon:), @[image, (id)mId]);
  }
}

void setTitle(char* ctitle) {
  NSString* title = [[NSString alloc] initWithCString:ctitle
                                             encoding:NSUTF8StringEncoding];
  free(ctitle);
  runInMainThread(@selector(setTitle:), (id)title);
}

void setTooltip(char* ctooltip) {
  NSString* tooltip = [[NSString alloc] initWithCString:ctooltip
                                               encoding:NSUTF8StringEncoding];
  free(ctooltip);
  runInMainThread(@selector(setTooltip:), (id)tooltip);
}

void setRemovalAllowed(bool allowed) {
  if (allowed) {
    runInMainThread(@selector(setRemovalAllowed), nil);
  } else {
    runInMainThread(@selector(setRemovalForbidden), nil);
  }
}

void add_or_update_menu_item(int menuId, int parentMenuId, char* title, char* tooltip, short disabled, short checked, short isCheckable) {
  MenuItem* item = [[MenuItem alloc] initWithId: menuId withParentMenuId: parentMenuId withTitle: title withTooltip: tooltip withDisabled: disabled withChecked: checked];
  free(title);
  free(tooltip);
  runInMainThread(@selector(add_or_update_menu_item:), (id)item);
}

void add_separator(int menuId, int parentId) {
  NSNumber *pId = [NSNumber numberWithInt:parentId];
  runInMainThread(@selector(add_separator:), (id)pId);
}

void hide_menu_item(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(hide_menu_item:), (id)mId);
}

void remove_menu_item(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(remove_menu_item:), (id)mId);
}

void show_menu() {
  runInMainThread(@selector(show_menu), nil);
}

void show_menu_item(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(show_menu_item:), (id)mId);
}

void reset_menu() {
  runInMainThread(@selector(reset_menu), nil);
}

void quit() {
  runInMainThread(@selector(quit), nil);
}
