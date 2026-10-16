# R8 rules for the shell.
#
# There is very little to keep here, and that is the point: the app's own code
# is reached from the manifest and from Android's own callbacks, both of which
# R8 already traces, and nothing in it is loaded reflectively. What follows is
# the two places where a name is looked up rather than called.

# The WebView's JavaScript bridge, if one is ever added, is resolved by name on
# the Java side and by the string the page uses. Keeping annotated members makes
# that survive shrinking rather than failing at runtime with a method-not-found
# that only shows up on a release build.
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}

# The Kernel's child process is started by absolute path, and its own log is
# read as text; neither is reflective, so nothing else needs keeping.

# Line numbers make a stack trace from a release build readable, which is the
# only way a crash report from a phone can be acted on.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# Kotlin's `@Metadata` annotation is present on every class the compiler touched
# and nothing in this app reflects over it. It is tempting to `-assumenosideeffects`
# it away, and that was tried: any pattern broad enough to name its members also
# matches the methods every class inherits from `Object` — `equals`, `hashCode`,
# `toString` — and R8 then treats those as removable, which is a silent
# behaviour change for a few kilobytes. Left as a warning suppression instead:
# the annotation is dropped from the shrunk output anyway, because nothing reads
# it.
-dontwarn kotlin.Metadata
