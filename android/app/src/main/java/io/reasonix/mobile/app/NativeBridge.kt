package io.reasonix.mobile.app

import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentValues
import android.content.Context
import android.content.res.Configuration
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import android.webkit.JavascriptInterface
import android.webkit.MimeTypeMap
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * What the page can ask the phone for.
 *
 * This is the whole surface, and it is deliberately tiny. Android's
 * `@JavascriptInterface` is a bridge into the app's process from a WebView whose
 * content is a web page: every method here is reachable by anything that can run
 * script in that page, so each one is written as a verb with no address
 * parameter it does not need, and none of them can name a path outside the
 * kernel's own home.
 *
 * It is deliberately *not* the Electron preload bridge. That one exposes window
 * management, a filesystem walker, an editor launcher and a browser view host —
 * all of which are meaningless on a phone, and the two that were not (open a URL,
 * save a file) already have a way to happen here: `WebViewClient` routes external
 * links to the browser, and the page's own download reaches the WebView's
 * `DownloadListener`.
 *
 * The one thing a phone must add is `importIntoHome`: the kernel runs on this
 * device and can only read paths under its own home, so a private key the person
 * has in their Documents folder has to be copied across. Nothing else the page
 * wants is out of its reach.
 */
class NativeBridge(
    private val context: Context,
    private val kernel: Kernel,
    private val onOpen: (String) -> Unit,
) {

    /** The version the page reads to decide whether this bridge is the one it
     *  knows. Bumped when a method's contract changes, not when one is added. */
    @JavascriptInterface
    fun version(): String = BRIDGE_VERSION

    /**
     * Copy a file the person picks into the kernel's home, answering with its
     * path there — or "" when they dismissed the picker.
     *
     * `kind` names the directory inside the home, because that is a fact about
     * the kernel's layout and not about the file: an SSH private key belongs in
     * `.ssh`, and a dotenv the kernel reads for passphrases belongs at the root
     * of the home where `config.Load` looks for it.
     *
     * The path that comes back is *inside the home*, never the document
     * provider's URI. That is the point: the kernel is a child process with no
     * access to the provider, so a `content://` would be a path it cannot open.
     */
    @JavascriptInterface
    fun importIntoHome(kind: String, name: String): String {
        val dir = when (kind) {
            "identity" -> File(kernel.homeDir(), ".ssh")
            "env" -> kernel.homeDir()
            else -> return ""
        }
        val safeName = sanitise(name)
        if (safeName.isEmpty()) return ""

        val picked = pickFile() ?: return ""
        return try {
            dir.mkdirs()
            val target = File(dir, safeName)
            context.contentResolver.openInputStream(picked)?.use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            } ?: return ""
            // A private key with group or world read is one OpenSSH refuses to
            // use, and the refusal reads as "bad permissions" from a long way
            // away. Setting it here is what makes the file usable at all.
            if (kind == "identity") {
                runCatching {
                    target.setReadable(false, false)
                    target.setReadable(true, true)
                    target.setWritable(false, false)
                    target.setWritable(true, true)
                }
            }
            target.absolutePath
        } catch (_: Exception) {
            // A copy that failed leaves nothing behind; the caller sees "" and
            // the person can try again.
            ""
        }
    }

    /**
     * Open a URL in the device's browser.
     *
     * The page asks for this when a link points somewhere outside the kernel —
     * a provider's console, a documentation page — and loading it in place would
     * strand the person in a window with no address bar and no way back.
     */
    @JavascriptInterface
    fun openExternal(url: String) {
        // Only what a browser can open. A `file://` or `content://` here would
        // be the page reaching into the device through a method named for
        // leaving it.
        if (!url.startsWith("http://") && !url.startsWith("https://")) return
        onOpen(url)
    }

    /** Put text on the clipboard. A WebView's own `navigator.clipboard` write is
     *  gated behind a permission Android grants only to a focused document, so
     *  the one thing the page actually offers to copy — a token, a command, a
     *  path — has no other way out.
     *
     *  Answers whether the write landed. A platform that refuses it raises
     *  rather than answering, and an exception must not cross
     *  `@JavascriptInterface` as a silent success — the page would say 已复制
     *  over an empty clipboard. Caught here, the answer is the one the caller
     *  can act on: false, and the button says 复制失败 instead. */
    @JavascriptInterface
    fun copyText(text: String): Boolean = runCatching {
        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("reasonix", text))
    }.isSuccess

    /**
     * Write text where the person picks, and answer with the path it went to.
     *
     * The page's own route to a file — a blob URL on an `<a download>` — reaches
     * nothing here: a WebView has no downloads shelf, and its `DownloadListener`
     * fires only for a real navigation, not for a blob the page just built. So
     * the text crosses the bridge and the shell opens the system's document
     * picker, which is also the only way to reach a folder the person can find
     * again afterwards.
     *
     * "" is a dismissal, the same answer `importIntoHome` gives, and the page
     * reads the two the same way.
     */
    /**
     * Put a copy of a file where a phone's own file manager will show it.
     *
     * `saveText` asks the person where to put it, which is the right question
     * on a desktop and the wrong one here: the kernel's workspace is
     * app-private storage, so the file someone wants is in a place no file
     * manager can open, and a picker that starts at "recent" is another place
     * to lose it. This answers the actual question — "where is it" — by
     * putting it in Downloads, in a folder named after the app, under a name
     * that is the file's own.
     *
     * The answer is the path relative to shared storage, so the page can say
     * where the file went rather than only that it worked. "" means the write
     * failed or the platform refused it.
     *
     * MediaStore on Android 10 and later, a plain file before that. The two
     * are not interchangeable: from 10 the app may not write another app's
     * files directly, and MediaStore is the only way in; before it there is no
     * MediaStore collection for downloads and the public directory is writable
     * with the storage permission this app does not ask for, so that branch is
     * best-effort and says so by answering "".
     */
    @JavascriptInterface
    fun exportToDownloads(name: String, content: String): String {
        val safeName = sanitise(name).ifEmpty { "reasonix.txt" }
        return exportFile(safeName, content).orEmpty()
    }

    @JavascriptInterface
    fun saveText(name: String, content: String): String {
        val safeName = sanitise(name).ifEmpty { "reasonix.txt" }
        return saveFile(safeName, content) ?: ""
    }

    /**
     * Hand text to whatever the device shares with — a chat app, the mail
     * client, a note app.
     *
     * The share sheet rather than a second save dialog: "where is that file"
     * is the question this answers, and on a phone a document someone wants to
     * keep or send usually goes through one of those apps rather than a folder.
     *
     * Shared as text with the filename as its subject, which is what every
     * target understands. A file attachment would need a `FileProvider` and a
     * cache copy for the same words to arrive.
     */
    @JavascriptInterface
    fun shareText(name: String, content: String): Boolean {
        val safeName = sanitise(name).ifEmpty { "reasonix.txt" }
        val posted = java.util.concurrent.CountDownLatch(1)
        var ok = false
        main.post {
            ok = runCatching { askForShare(safeName, content) }.getOrDefault(false)
            posted.countDown()
        }
        runCatching { posted.await(SHARE_TIMEOUT_SECONDS, TimeUnit.SECONDS) }
        return ok
    }

    /**
     * The device's own light/dark answer.
     *
     * The page resolves its theme from `localStorage` first and only asks the
     * system when nothing is saved, so this is what makes a fresh install match
     * the phone instead of defaulting to light.
     */
    @JavascriptInterface
    fun systemTheme(): String {
        val night = context.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK
        return if (night == Configuration.UI_MODE_NIGHT_YES) "dark" else "light"
    }

    /**
     * The window's real safe-area insets, in CSS pixels, as
     * `top,right,bottom,left`.
     *
     * A WebView does not implement `env(safe-area-inset-*)`: those are an iOS
     * Safari feature, and on Android every one of them resolves to `0`. So a
     * page that pads itself with them — which `mobile.css` does — is un-padded
     * on exactly the platform this shell exists for, and the first line of the
     * interface sits under the status bar.
     *
     * The values have to come from here for a second reason too: from Android 15
     * the window is edge-to-edge whether the app asks for it or not, so the bars
     * really are drawn over the page and there is no window-manager switch that
     * could move the content back down.
     *
     * Answered as a string because that is what crosses the bridge; the page
     * sets it as a CSS custom property and `mobile.css` reads it.
     */
    @JavascriptInterface
    fun safeArea(): String {
        val insets = onInsets?.invoke() ?: return "0,0,0,0"
        return "${insets.left},${insets.top},${insets.right},${insets.bottom}"
    }

    /**
     * Read the pairing code a computer is showing.
     *
     * A phone cannot type what a computer renders as a QR code — it is a
     * one-time code inside a URL, and the whole reason it is a picture is that
     * nobody reads one aloud. So the page asks for the camera and gets the text
     * back, which is the same direction the computer's own dialog describes.
     *
     * The answer is the code's own text, not a parsed result: what a pairing
     * fragment means is the page's to decide, and it already knows how — the
     * `#pair=` handling ships with the interface.
     */
    @JavascriptInterface
    fun scanPairingCode(): String = scanWithCamera()

    /**
     * Load a URL in this window instead of handing it to the browser.
     *
     * The page asks for this once, right after a scan: the code it read points at
     * the computer's own Studio, and the whole point of scanning is to end up
     * looking at that computer. Going out to a browser would leave the app on one
     * screen and the paired machine on another, with the back gesture an exit
     * rather than a return.
     *
     * Only http and https are accepted. The page is this repository's own build
     * and is trusted, but a scanner reads whatever is held in front of it, so the
     * one value that arrives here from outside is checked: a `file://` or
     * `javascript:` that happened to be on a printed code must not become a
     * navigation.
     */
    @JavascriptInterface
    fun openInApp(url: String) {
        val trimmed = url.trim()
        if (!trimmed.startsWith("http://") && !trimmed.startsWith("https://")) return
        main.post { onOpenInApp?.invoke(trimmed) }
    }

    /** How the activity loads a URL in the window it owns. Set by the activity. */
    var onOpenInApp: ((String) -> Unit)? = null

    // ---- the native browser view host bridge ----
    //
    // The page's `host.ts` looks for `window.reasonixHost` with Promise-based
    // methods (showBrowserView, hideBrowserView, etc.). `@JavascriptInterface`
    // is synchronous, so the round trip is: JS generates an id, calls
    // `hostCall(id, method, args)`, Kotlin does the work asynchronously, then
    // pushes the result back via `evaluateJavascript` calling
    // `window.__reasonixHostReply(id, json)`. The injected script (in
    // MainActivity) wires each Promise to that reply channel.

    /** How the activity handles an async host call. Set by the activity. */
    var onHostCall: ((id: String, method: String, args: String) -> Unit)? = null

    /**
     * Entry point for the page's `window.reasonixHost` Promise methods.
     *
     * `id` is a unique call identifier the JS side generated; `method` is the
     * method name (e.g. "showBrowserView"); `args` is the JSON-encoded argument
     * array. The activity handles the call asynchronously and replies through
     * `evaluateJavascript` with `window.__reasonixHostReply(id, resultJson)`.
     */
    @JavascriptInterface
    fun hostCall(id: String, method: String, args: String) {
        onHostCall?.invoke(id, method, args)
    }

    /**
     * Whether the page has a panel up that the back gesture should close first.
     *
     * Written by the page, read by the activity's back handler. The page owns the
     * answer — it is the only thing that knows what is open, and a second
     * implementation of that question in the shell would be one that drifts — so
     * the page reports the fact and the shell keeps the action.
     *
     * Volatile because the two ends are on different threads: the page sets it
     * from the WebView's JavaScript thread, the back gesture reads it on the main
     * one, and a stale read here is a panel that does not close.
     */
    @Volatile
    var layerOpen: Boolean = false
        private set

    @JavascriptInterface
    fun setLayerOpen(open: Boolean) {
        layerOpen = open
    }

    // ---- the picker, which is an Activity result in a synchronous world ----

    private val pending = AtomicReference<Uri?>(null)
    private val latch = AtomicReference<CountDownLatch?>(null)

    /** The code the camera read, held between the activity's answer and the
     *  wait below — the same round trip the file picker takes. */
    private val scanned = AtomicReference<String?>(null)
    private val scanLatch = AtomicReference<CountDownLatch?>(null)

    /** The main thread, because starting an Activity result must happen there. */
    private val main = Handler(Looper.getMainLooper())

    /**
     * How to ask for a file. Set by the activity, which owns the Activity Result
     * API; it answers whether the ask was dispatched, not whether the person
     * picked anything — that arrives through `deliver`.
     */
    var askForFile: () -> Boolean = { false }

    /**
     * How the activity reports the window's insets. Set by the activity, which is
     * the only thing that can see them, and asked on every call rather than
     * cached — a rotation, a fold, or the keyboard opening all change them, and a
     * stale answer here is a stripe of interface under the status bar.
     */
    var onInsets: (() -> Insets?)? = null

    /**
     * How to open the camera. Set by the activity, which owns the Activity
     * Result API; like `askForFile` it answers whether the ask was dispatched,
     * and the code itself arrives through `deliverScan`.
     */
    var askForScan: () -> Boolean = { false }

    /**
     * How to put a file where the person picks. Set by the activity, which owns
     * the Activity Result API; it answers whether the ask was dispatched, and
     * the chosen place arrives through `deliverSave`.
     */
    var askForSave: (name: String, content: String) -> Boolean = { _, _ -> false }

    /**
     * How to open the share sheet. Set by the activity, which is the only thing
     * that can start an Intent; it answers whether anything on the device took
     * the text.
     */
    var askForShare: (name: String, content: String) -> Boolean = { _, _ -> false }

    /** The place the save dialog answered with, held between the two halves. */
    private val saveTarget = AtomicReference<Uri?>(null)
    private val saveLatch = AtomicReference<CountDownLatch?>(null)

    /**
     * Ask where to put a file and write it there.
     *
     * The same latch round trip as `pickFile`, and for the same reason:
     * `@JavascriptInterface` cannot suspend, so the WebView's thread waits while
     * the picker is open — a modal panel the person is looking at.
     */
    private fun saveFile(name: String, content: String): String? {
        val gate = CountDownLatch(1)
        saveLatch.set(gate)
        saveTarget.set(null)

        val posted = CountDownLatch(1)
        var dispatched = false
        main.post {
            dispatched = runCatching { askForSave(name, content) }.getOrDefault(false)
            posted.countDown()
        }
        val ready = runCatching { posted.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS) }.getOrDefault(false)
        if (!ready || !dispatched) {
            saveLatch.set(null)
            return null
        }
        return try {
            if (!gate.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS)) null
            else saveTarget.get()?.toString()
        } catch (_: InterruptedException) {
            null
        } finally {
            saveLatch.set(null)
        }
    }

    /** Called by the activity once the bytes are written. A null uri is a dismissal. */
    fun deliverSave(uri: Uri?) {
        saveTarget.set(uri)
        saveLatch.get()?.countDown()
    }

    /**
     * Read the pairing code a computer is showing, and return it.
     *
     * The same shape as `pickFile` and for the same reason: a
     * `@JavascriptInterface` method is called on the WebView's thread and must
     * return a value, so the camera is opened on the main thread and this one
     * waits. The block lasts as long as the person is aiming, which is why the
     * timeout is generous — a scan that gave up after a few seconds would be
     * one nobody could complete.
     *
     * An empty string is "no code", which is what a refusal, a cancellation and
     * a timeout all are. The caller acts on the text, so a distinct error would
     * only be a fourth way to say the same thing.
     */
    private fun scanWithCamera(): String {
        val gate = CountDownLatch(1)
        scanLatch.set(gate)
        scanned.set(null)

        val posted = CountDownLatch(1)
        var dispatched = false
        main.post {
            dispatched = runCatching { askForScan() }.getOrDefault(false)
            posted.countDown()
        }
        val ready = runCatching { posted.await(SCAN_TIMEOUT_SECONDS, TimeUnit.SECONDS) }.getOrDefault(false)
        if (!ready || !dispatched) {
            scanLatch.set(null)
            return ""
        }
        return try {
            if (!gate.await(SCAN_TIMEOUT_SECONDS, TimeUnit.SECONDS)) "" else scanned.get().orEmpty()
        } catch (_: InterruptedException) {
            ""
        } finally {
            scanLatch.set(null)
        }
    }

    /** Called by the activity when the scanner answers. Null is a dismissal. */
    fun deliverScan(text: String?) {
        scanned.set(text)
        scanLatch.get()?.countDown()
    }

    /**
     * Ask for one file and wait for the answer.
     *
     * `@JavascriptInterface` methods cannot suspend: the WebView calls them on
     * its own thread and expects a value back. So the ask is posted to the main
     * thread and this one waits on a latch. The cost is a blocked JS thread for
     * as long as the picker is open — which is a modal panel the person is
     * looking at, with nothing else in the page running.
     *
     * A timeout is set rather than waiting forever: a picker that never answers
     * (the activity destroyed underneath it) would otherwise pin a WebView
     * thread until the process died.
     */
    private fun pickFile(): Uri? {
        val gate = CountDownLatch(1)
        latch.set(gate)
        pending.set(null)

        val posted = CountDownLatch(1)
        var dispatched = false
        main.post {
            dispatched = runCatching { askForFile() }.getOrDefault(false)
            posted.countDown()
        }
        val ready = runCatching { posted.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS) }.getOrDefault(false)
        if (!ready || !dispatched) {
            latch.set(null)
            return null
        }

        return try {
            if (!gate.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS)) null else pending.get()
        } catch (_: InterruptedException) {
            null
        } finally {
            latch.set(null)
        }
    }

    /** Called by the activity when the picker answers. A null uri is a dismissal. */
    fun deliver(uri: Uri?) {
        pending.set(uri)
        latch.get()?.countDown()
    }

    /** The folder under Downloads that this app's exports land in. */
    private fun exportFolder(): String = "Reasonix"

    /** The MIME type this name declares, or null when the platform does not know
     *  it.
     *
     * Where the type is written, the name and the type agree, and that agreement
     * is the whole fix: `.md` is answered `text/markdown` here, so MediaStore
     * has nothing to correct and the export stays `报告.md` instead of being
     * renamed `报告.md.txt` by the `text/plain` it used to be given. Where the
     * type is unknown the field is left off, which leaves MediaStore to derive
     * the same `application/octet-stream` it would have been told — the name
     * still stands on its own, which is all this is for. */
    private fun mimeFor(name: String): String? {
        val ext = name.substringAfterLast('.', "").lowercase()
        if (ext.isEmpty() || ext == name.lowercase()) return null
        return runCatching { MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) }.getOrNull()
    }

    private fun exportFile(name: String, content: String): String? = runCatching {
        val bytes = content.toByteArray(Charsets.UTF_8)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val resolver = context.contentResolver
            val values = ContentValues().apply {
                put(MediaStore.Downloads.DISPLAY_NAME, name)
                // The type is derived here rather than declared, and it is
                // written only when the name really has an extension that maps
                // to one.
                //
                // It used to be `text/plain` for everything, and MediaStore
                // answers a name whose extension disagrees with the declared
                // type by appending its own: exporting `报告.md` landed in
                // Downloads as `报告.md.txt`, and any source file came out with
                // the wrong second extension. Deriving the type from the name
                // instead keeps the two in agreement — which is what stops the
                // rename — and a name the platform has no type for is left off
                // entirely rather than declared as `application/octet-stream`,
                // so nothing is corrected there either.
                mimeFor(name)?.let { put(MediaStore.Downloads.MIME_TYPE, it) }
                put(
                    MediaStore.Downloads.RELATIVE_PATH,
                    Environment.DIRECTORY_DOWNLOADS + "/" + exportFolder(),
                )
                // Hidden until the bytes are all there, so a file manager that
                // reads the folder mid-write cannot show half a document.
                put(MediaStore.Downloads.IS_PENDING, 1)
            }
            val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
                ?: return@runCatching null
            resolver.openOutputStream(uri)?.use { it.write(bytes) } ?: return@runCatching null
            values.clear()
            values.put(MediaStore.Downloads.IS_PENDING, 0)
            resolver.update(uri, values, null, null)
            "${Environment.DIRECTORY_DOWNLOADS}/${exportFolder()}/$name"
        } else {
            val dir = File(
                Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS),
                exportFolder(),
            )
            if (!dir.exists() && !dir.mkdirs()) return@runCatching null
            val target = File(dir, name)
            target.writeBytes(bytes)
            target.absolutePath
        }
    }.getOrNull()

    private fun sanitise(name: String): String {
        // A name from the page is used as a filename, so it is reduced to one:
        // no separators, no traversal, and nothing a filesystem treats as
        // special. The default is the name the field it came from suggests.
        val trimmed = name.trim().substringAfterLast('/').substringAfterLast('\\')
        if (trimmed.isEmpty() || trimmed == "." || trimmed == "..") return ""
        return trimmed.filter { it.isLetterOrDigit() || it in "._-" }.take(64)
    }

    /**
     * Whether a turn is running, so the screen can be kept awake for one.
     *
     * The page owns the answer — it is the only thing that knows if the kernel
     * is working — and the shell owns the flag, because a window flag is not
     * something a page can set. Nothing is asked at launch: a window that is
     * merely open is one the person may be reading, and holding the screen on
     * for that is holding it on for nothing.
     */
    @JavascriptInterface
    fun setRunning(running: Boolean) {
        onRunning?.invoke(running)
    }

    /** How the activity applies that flag. Set by the activity. */
    var onRunning: ((Boolean) -> Unit)? = null

    /**
     * Whether the app may read every file, which is what lets the kernel open a
     * folder on this phone as a workspace.
     *
     * Asked rather than assumed: the permission is granted by hand in system
     * settings, so the page has to be able to say "not yet" rather than fail on
     * the first `stat`. Answered `true` below API 30, where ordinary read
     * permission is enough and the concept does not exist.
     */
    @JavascriptInterface
    fun hasAllFilesAccess(): Boolean = filesAccess?.invoke() ?: true

    var filesAccess: (() -> Boolean)? = null

    /** Open the system page where that permission is granted. The only place
     *  Android offers it — there is no dialog for this one. */
    @JavascriptInterface
    fun requestAllFilesAccess() {
        askForFilesAccess?.invoke()
    }

    var askForFilesAccess: (() -> Unit)? = null

    /**
     * Whether this app is exempt from the system's battery optimisation.
     *
     * A turn runs in a child process under a foreground service. Under doze the
     * whole app group is frozen anyway, so the turn stops making progress while
     * the screen is off — which is the state the person left it in on purpose.
     * Unrestricted is the answer, and it is a question Android answers.
     */
    @JavascriptInterface
    fun batteryUnrestricted(): Boolean = batteryFree?.invoke() ?: true

    var batteryFree: (() -> Boolean)? = null

    /** Ask for that exemption. Unlike the storage one, this has a real dialog. */
    @JavascriptInterface
    fun requestBatteryUnrestricted() {
        askForBattery?.invoke()
    }

    var askForBattery: (() -> Unit)? = null

    /**
     * Ask for a folder on this phone and answer with its real filesystem path.
     *
     * "" is a dismissal, the same answer `importIntoHome` gives — and, unlike
     * that one, an empty string is a normal outcome here rather than a failure.
     * The `startIn` hint is a path the kernel knows; the picker may ignore it,
     * which is the picker's business.
     *
     * The round trip is the same latch as `pickFile`, for the same reason: a
     * document-tree picker is another activity, and this call cannot suspend.
     */
    @JavascriptInterface
    fun pickFolder(startIn: String): String {
        val gate = CountDownLatch(1)
        folderLatch.set(gate)
        folderPending.set(null)

        val posted = CountDownLatch(1)
        var dispatched = false
        main.post {
            dispatched = runCatching { askForFolder(startIn) }.getOrDefault(false)
            posted.countDown()
        }
        val ready = runCatching { posted.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS) }.getOrDefault(false)
        if (!ready || !dispatched) {
            folderLatch.set(null)
            return ""
        }
        return try {
            if (!gate.await(PICK_TIMEOUT_SECONDS, TimeUnit.SECONDS)) "" else folderPending.get().orEmpty()
        } catch (_: InterruptedException) {
            ""
        } finally {
            folderLatch.set(null)
        }
    }

    var askForFolder: (String) -> Boolean = { false }

    private val folderPending = AtomicReference<String?>(null)
    private val folderLatch = AtomicReference<CountDownLatch?>(null)

    /** Called by the activity with the folder's real path, or null on dismissal. */
    fun deliverFolder(path: String?) {
        folderPending.set(path ?: "")
        folderLatch.get()?.countDown()
    }

    companion object {
        const val BRIDGE_VERSION = "4"
        private const val PICK_TIMEOUT_SECONDS = 5L * 60

        /** Long, because this is however long someone takes to aim. A shorter
         *  one would turn "still lining it up" into a failed scan. */
        private const val SCAN_TIMEOUT_SECONDS = 5L * 60

        /** Only as long as the sheet takes to appear: the answer is "something
         *  took it", which is known the moment the Intent resolves. */
        private const val SHARE_TIMEOUT_SECONDS = 15L
    }
}

/**
 * The window's insets, in CSS pixels.
 *
 * A named type rather than four numbers because the page reads them as one
 * thing: half an inset is a layout that is wrong in one direction, and a caller
 * that passed the wrong pair would be indistinguishable from a device with no
 * cutout at all.
 */
data class Insets(val left: Int, val top: Int, val right: Int, val bottom: Int)
