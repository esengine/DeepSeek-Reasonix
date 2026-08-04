//go:build darwin && cgo

#import <Foundation/Foundation.h>

#include <stdlib.h>
#include <string.h>

static void reasonix_set_error(char **output, NSError *error, NSString *fallback) {
    if (output == NULL) {
        return;
    }
    NSString *message = error.localizedDescription ?: fallback;
    *output = strdup(message.UTF8String ?: "unknown macOS alias error");
}

static BOOL reasonix_is_alias(NSURL *url, NSError **error) {
    NSNumber *value = nil;
    if (![url getResourceValue:&value forKey:NSURLIsAliasFileKey error:error]) {
        return NO;
    }
    return value.boolValue;
}

static int reasonix_create_alias(NSURL *targetURL, NSURL *aliasURL, char **error_message) {
    NSError *error = nil;
    NSData *bookmark = [targetURL bookmarkDataWithOptions:NSURLBookmarkCreationSuitableForBookmarkFile
                           includingResourceValuesForKeys:nil
                                            relativeToURL:nil
                                                    error:&error];
    if (bookmark == nil) {
        reasonix_set_error(error_message, error, @"create Finder alias bookmark");
        return -1;
    }
    if (![NSURL writeBookmarkData:bookmark toURL:aliasURL options:0 error:&error]) {
        reasonix_set_error(error_message, error, @"write Finder alias bookmark");
        return -1;
    }
    return 0;
}

int reasonix_write_alias(const char *target_path, const char *alias_path,
                         char **error_message) {
    @autoreleasepool {
        NSURL *targetURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:target_path]];
        NSURL *aliasURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:alias_path]];
        return reasonix_create_alias(targetURL, aliasURL, error_message);
    }
}

int reasonix_resolve_alias(const char *alias_path, char **target_path,
                           char **error_message) {
    @autoreleasepool {
        NSError *error = nil;
        NSURL *aliasURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:alias_path]];
        NSURL *resolved = [NSURL URLByResolvingAliasFileAtURL:aliasURL
                                                     options:NSURLBookmarkResolutionWithoutUI |
                                                             NSURLBookmarkResolutionWithoutMounting
                                                       error:&error];
        if (resolved == nil) {
            reasonix_set_error(error_message, error, @"resolve Finder alias");
            return -1;
        }
        if (target_path != NULL) {
            *target_path = strdup(resolved.path.UTF8String);
        }
        return 0;
    }
}

int reasonix_repair_alias(const char *alias_path, const char *current_app_path,
                          int allow_broken, char **error_message) {
    @autoreleasepool {
        NSError *error = nil;
        NSURL *aliasURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:alias_path]];
        if (!reasonix_is_alias(aliasURL, &error)) {
            // Missing metadata means this is an ordinary desktop file, not an
            // owned integration failure. Never replace it merely by name.
            return 0;
        }

        NSURL *currentURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:current_app_path]];
        NSURL *resolved = [NSURL URLByResolvingAliasFileAtURL:aliasURL
                                                     options:NSURLBookmarkResolutionWithoutUI |
                                                             NSURLBookmarkResolutionWithoutMounting
                                                       error:&error];
        BOOL owned = NO;
        if (resolved != nil) {
            NSString *resolvedPath = resolved.URLByResolvingSymlinksInPath.standardizedURL.path;
            NSString *currentPath = currentURL.URLByResolvingSymlinksInPath.standardizedURL.path;
            if ([resolvedPath isEqualToString:currentPath]) {
                // The alias is already healthy. Avoid rewriting it on every
                // launch so Finder metadata and any user customization remain.
                return 0;
            } else {
                NSBundle *bundle = [NSBundle bundleWithURL:resolved];
                owned = [bundle.bundleIdentifier isEqualToString:@"com.wails.reasonix-desktop"];
            }
        } else if (allow_broken != 0) {
            // A broken alias has no resolvable target to prove ownership. Only
            // the exact historical installer names are admitted by Go.
            owned = YES;
        }
        if (!owned) {
            return 0;
        }

        NSURL *directory = [aliasURL URLByDeletingLastPathComponent];
        NSString *temporaryName = [NSString stringWithFormat:@".reasonix-alias-%@", NSUUID.UUID.UUIDString];
        NSURL *temporaryURL = [directory URLByAppendingPathComponent:temporaryName];
        if (reasonix_create_alias(currentURL, temporaryURL, error_message) != 0) {
            return -1;
        }

        NSURL *resultingURL = nil;
        if (![[NSFileManager defaultManager] replaceItemAtURL:aliasURL
                                                withItemAtURL:temporaryURL
                                               backupItemName:nil
                                                      options:NSFileManagerItemReplacementUsingNewMetadataOnly
                                             resultingItemURL:&resultingURL
                                                        error:&error]) {
            [[NSFileManager defaultManager] removeItemAtURL:temporaryURL error:nil];
            reasonix_set_error(error_message, error, @"replace Finder alias");
            return -1;
        }
        return 1;
    }
}
