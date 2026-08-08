//go:build darwin

#import <Cocoa/Cocoa.h>

int reasonixAppIsActive(void) {
	return [NSApp isActive] ? 1 : 0;
}
