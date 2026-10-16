package io.reasonix.mobile.app

import android.annotation.SuppressLint
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.PowerManager
import android.provider.DocumentsContract
import android.provider.Settings
import android.util.Base64
import android.util.Log
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.webkit.CookieManager
import android.webkit.DownloadListener
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.ProgressBar
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.URL
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.json.JSONArray
import org.json.JSONObject

/**
 * The window: one WebView on the kernel's loopback origin.
 *
 * Almost everything the person does is the page's, and deliberately so — the
 * interface is the same build the desktop Studio and `reasonix web` serve, which
 * is what keeps one product rather than three. What this class adds is the four
 * things a page cannot do for itself on a phone:
 *
 *  - the system back gesture means "go back in the page", and only means "leave"
 *    when there is nowhere left to go;
 *  - a download goes to the system's file picker, because a WebView has no
 *    downloads shelf and app-private storage is not where a person looks;
 *  - a link to somewhere else opens in the real browser, where the person's
 *    logins and tabs are;
 *  - the kernel is started before the page is asked for, and kept alive by the
 *    service, because a phone kills a backgrounded child process in seconds.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var web: WebView
    private lateinit var splash: View

    /** The page's way into the phone. One per window, since a WebView is. */
    private lateinit var bridge: NativeBridge

    /** set once the first page is up, so a reload does not go through the splash */
    private var booted = false

    /** The download waiting for a place to go, held between the two halves of
     *  the system picker's round trip. */
    private var pendingDownload: PendingDownload? = null

    /**
     * The origin of a page loaded into this window from somewhere other than the
     * kernel — a computer paired by scanning its code.
     *
     * Held so navigation inside that page stays in here. Without it every link
     * on the paired computer's Studio would be handed to the browser, which is
     * the one place it must not go: the pairing cookie lives in this WebView's
     * jar, so a link opened outside would arrive unpaired.
     */
    private var inAppOrigin: String? = null

    /** The root layout, held so the overlay WebView can be added to it. */
    private lateinit var rootLayout: FrameLayout

    /** The overlay WebView the kernel's browser tools drive through the
     *  shell's loopback HTTP server. Created lazily on the first
     *  showBrowserView call. */
    private var browserView: WebView? = null

    /** The loopback HTTP server the kernel's browser tools talk to. */
    private var browserShellServer: BrowserShellServer? = null

    /** The last main-frame load that failed, as `(url, chromium error)`, cleared
     *  when a fresh navigation starts. The browser's own error page has no site
     *  content in it, so this is what lets `/read` answer with the reason
     *  instead of quoting the browser's notice as though the site had said it. */
    @Volatile
    private var browserLoadFailure: Pair<String, String>? = null

    /** A navigation the browser shell is waiting to settle: counted down when
     *  the page arrives or its load fails, and replaced by each new navigation.
     *  `/load` answers as soon as it has started the load, so a `/read` right
     *  behind it used to describe the page before — and hand back that page's
     *  failure as the new answer. Reading waits on this instead. */
    @Volatile
    private var browserNavLatch: CountDownLatch? = null

    /** What the page in the overlay WebView has asked the network for, oldest
     *  first, cleared when a new navigation starts. The interceptor behind this
     *  is handed a request's method and URL and nothing about its response, so
     *  this is all the browser tools may report. Bounded like the desktop
     *  backend's list: a page that polls makes thousands of requests. */
    private val browserRequests = ArrayList<BrowserRequest>()

    private data class PendingDownload(val url: String, val userAgent: String, val mime: String, val name: String)

    private val saveDownload = registerForActivityResult(ActivityResultContracts.CreateDocument("*/*")) { target ->
        val job = pendingDownload ?: return@registerForActivityResult
        pendingDownload = null
        if (target == null) return@registerForActivityResult
        // The fetch is the WebView's, because the cookie that authorises it is
        // the WebView's: the kernel's download route is behind the same session
        // the page is using, and the shell has no cookie jar of its own.
        lifecycleScope.launch {
            val result = Downloader.run(this@MainActivity, job.url, job.userAgent, job.mime, job.name, target)
            Downloader.report(this@MainActivity, job.name, result)
        }
    }

    /** The text waiting for a place to go, held between the two halves of the
     *  save dialog's round trip. */
    private var pendingSave: Pair<String, String>? = null

    /**
     * Where the page's text landed.
     *
     * The page cannot do this itself: a WebView has no downloads shelf, and its
     * `DownloadListener` fires only for a real navigation — not for a blob the
     * page just built. So the bytes come across the bridge and are written here,
     * to the place the system's picker answered with.
     *
     * A null target is a dismissal, and the bridge reads it as "" the same way
     * it reads a dismissed file picker.
     */
    private val saveTextFile = registerForActivityResult(ActivityResultContracts.CreateDocument("*/*")) { target ->
        val job = pendingSave
        pendingSave = null
        if (target == null) {
            bridge.deliverSave(null)
            return@registerForActivityResult
        }
        val written = runCatching {
            contentResolver.openOutputStream(target, "wt")?.use { out ->
                out.write(job?.second.orEmpty().toByteArray(Charsets.UTF_8))
            } ?: throw java.io.FileNotFoundException(target.toString())
        }
        if (written.isFailure) {
            Toast.makeText(this, getString(R.string.download_failed, written.exceptionOrNull()?.message.orEmpty()), Toast.LENGTH_LONG).show()
        }
        bridge.deliverSave(target)
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        rootLayout = FrameLayout(this)
        web = WebView(this)
        rootLayout.addView(
            web,
            FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT),
        )
        splash = layoutInflater.inflate(R.layout.view_splash, rootLayout, false)
        rootLayout.addView(
            splash,
            FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT),
        )
        setContentView(rootLayout)

        configureWebView()
        // Keep the renderer alive while the app is off screen.
        //
        // A WebView's default policy is `RENDERER_PRIORITY_BOUND` with
        // `waivedWhenNotVisible` true, which tells Android the renderer may be
        // reclaimed the moment the window is not in front. On this app that is
        // the wrong answer twice over: the page is the only place the running
        // turn is shown, and rebuilding it means re-subscribing the event stream
        // from scratch — so a phone pocketed mid-answer came back to a
        // transcript that had stopped growing. `IMPORTANT` with waiving off says
        // the renderer is doing work the person asked for, which is what the
        // foreground service says about the kernel beside it.
        //
        // Called on the view rather than inside `configureWebView`: that helper's
        // receiver is the `WebSettings` object, and this method is the WebView's.
        //
        // The cost is real and bounded: one renderer's memory, held while the app
        // is backgrounded. It is the same renderer the foreground service already
        // keeps the kernel for.
        web.setRendererPriorityPolicy(WebView.RENDERER_PRIORITY_IMPORTANT, false)
        installBridge(web)
        // The native browser view: a second WebView the kernel drives through
        // a loopback HTTP server, overlaid on the page when the browser panel
        // asks for one. Started before the kernel because the kernel reads the
        // port from an env var at launch.
        browserShellServer = BrowserShellServer(
            { browserView },
            ::ensureBrowserView,
            { browserLoadFailure },
            ::beginBrowserNavigation,
            { synchronized(browserShellLock) { browserRequests.toList() } },
            ::awaitBrowserNavigation,
        )
        runCatching { browserShellServer?.start() }
            .onFailure { Log.w("MainActivity", "browser shell server failed: ${it.message}") }
        // Hand the kernel the port and credential *before* anything can start
        // it. `KernelService.start` below launches the kernel too, and whichever
        // caller arrives first is the one that really launches it — so setting
        // these afterwards left the order to luck: a kernel started by the
        // service first would have no `REASONIX_SHELL_BROWSER_PORT`, and would
        // keep its browser tools suspended until the next launch. Both facts are
        // known here, so they are published here. (The refusal the phone
        // reported had a different cause — the server bound IPv6 while the
        // kernel dialed IPv4; see `BrowserShellServer.start`.)
        val kernel = (application as App).kernel
        kernel.browserShellPort = browserShellServer?.port ?: 0
        kernel.browserShellToken = browserShellServer?.token.orEmpty()
        // The insets have to be read from the window and pushed into the page:
        // `env(safe-area-inset-*)` is an iOS Safari feature and a WebView answers
        // it with 0, so without this the interface starts under the status bar.
        installInsetsListener()
        ViewCompat.requestApplyInsets(web)

        // The kernel runs under the service, so it survives the screen going off
        // and a task that takes minutes finishes. Starting it here as well is
        // what makes the app usable in one tap: the service is the guarantee,
        // this is the path.
        KernelService.start(this)

        lifecycleScope.launch {
            kernel.start()
                .onSuccess { load(it) }
                .onFailure { showFailure(it) }
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                // Three meanings, innermost first. The gesture means "go back",
                // and what back is depends on what is in front of the person:
                //
                //   1. a panel is up — close it, which is what Escape does on a
                //      desktop and what the page already knows how to unwind;
                //   2. otherwise step back through the page's own history;
                //   3. otherwise leave, and leave *to the background* rather
                //      than exiting.
                //
                // The last one is the one a phone expects and the one this app
                // did not do: `finish()` on a WebView app destroys the window and
                // stops the foreground service with it, so a turn that was part
                // way through died on a stray back press. Moving to the back of
                // the task leaves the kernel running — the service exists for
                // exactly this — and the next launch is instant.
                if (bridge.layerOpen) {
                    web.evaluateJavascript("window.__reasonixBack && window.__reasonixBack();", null)
                    return
                }
                if (web.canGoBack()) {
                    web.goBack()
                    return
                }
                moveTaskToBack(true)
            }
        })
    }

    private fun configureWebView() = with(web.settings) {
        javaScriptEnabled = true
        // localStorage holds the theme, the language and the session filter —
        // all of it the page's own state, none of it a credential.
        domStorageEnabled = true
        databaseEnabled = true
        // The page is served over HTTP from loopback; file access would add a
        // second, weaker origin for no gain, so it stays off.
        allowFileAccess = false
        allowContentAccess = false
        allowFileAccessFromFileURLs = false
        allowUniversalAccessFromFileURLs = false
        setSupportZoom(false)
        builtInZoomControls = false
        displayZoomControls = false
        // The page is responsive and sets its own viewport; a WebView that also
        // fits content to the screen fights it and produces a layout neither
        // intends.
        useWideViewPort = true
        loadWithOverviewMode = false
        mediaPlaybackRequiresUserGesture = false
        cacheMode = WebSettings.LOAD_DEFAULT
        // Cookies carry the session the page traded its fragment for. Without
        // them a reload loses the credential and the kernel refuses the page.
        CookieManager.getInstance().setAcceptCookie(true)
        // Not third-party: the page carries a first-party credential and nothing
        // it loads is cross-origin, so accepting them would only widen what a
        // later change could reach.
        CookieManager.getInstance().setAcceptThirdPartyCookies(web, false)
    }

    private val pickIdentity = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        // The bridge is waiting on a latch inside `importIntoHome`; answering it
        // is the whole job here. A null uri is a dismissed picker, which the
        // bridge reads as "" — an answer, not a failure.
        bridge.deliver(uri)
    }

    /**
     * The file an `<input type="file">` on the page asked for.
     *
     * A WebView does not open a picker by itself. The page's input fires
     * `onShowFileChooser` on the chrome client, and a client that does not
     * override it answers "nothing happened" — which is exactly how the
     * composer's attachment button read: a tap that produced no picker and no
     * error. Holding the callback here is the other half: the picker is a
     * separate activity, so the answer arrives later and has to be handed back
     * to the one call that is waiting on it.
     */
    private var fileChooserCallback: ValueCallback<Array<Uri>>? = null

    private val fileChooser = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        // Cleared before the answer is delivered: a second tap while a picker
        // was open replaces the callback, and the stale one has to be released
        // rather than left waiting for a result that will never come.
        val callback = fileChooserCallback ?: return@registerForActivityResult
        fileChooserCallback = null
        callback.onReceiveValue(parseChooserResult(result.resultCode, result.data))
    }

    /**
     * The URIs a file picker answered with, or null when nothing was chosen.
     *
     * `FileChooserParams.parseResult` is deliberately not used here. Its
     * WebView implementation answers null whenever the result carries no
     * `Intent.getData()`, which is exactly how a *multi*-selection arrives: the
     * picks are in the clip data and the single-item `data` field is left
     * unset. So choosing three files used to read the same as choosing none —
     * the attachment button stayed empty — while choosing one worked, because
     * then `data` is what the picker fills in. Reading the clip data first is
     * the fix; `data` stays as the fallback for the pickers that answer that
     * way. Duplicates are dropped, since a picker is free to put the same item
     * in both fields.
     */
    private fun parseChooserResult(resultCode: Int, data: Intent?): Array<Uri>? {
        if (resultCode != Activity.RESULT_OK || data == null) return null
        val picked = LinkedHashSet<Uri>()
        data.clipData?.let { clip ->
            for (index in 0 until clip.itemCount) clip.getItemAt(index)?.uri?.let(picked::add)
        }
        data.data?.let(picked::add)
        return picked.takeIf { it.isNotEmpty() }?.toTypedArray()
    }

    /**
     * A folder on this phone, as a workspace.
     *
     * `OpenDocumentTree` is the only system picker for a directory, and it
     * answers with a tree URI — not the path the kernel opens. The translation
     * below is the one that makes it usable: the tree's document id names the
     * volume and the relative folder, and with every-file access granted those
     * map onto a real directory the kernel can `stat`.
     *
     * That mapping is why the permission is asked for first. Without it the path
     * is real but unreadable, and the kernel reports an empty or missing folder
     * — a failure a long way from its cause.
     */
    private val pickFolderTree = registerForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        bridge.deliverFolder(uri?.let(::realPathFromTree))
    }

    /**
     * A tree URI as the filesystem path a child process can open, or null when
     * the volume is one this mapping does not cover (a cloud or MTP provider).
     *
     * The path is checked before it is handed over, because the mapping is a
     * guess about a naming convention rather than a lookup: the document id is
     * the volume and the folder *below* it, and this turns that into a string.
     * A tree whose id carries `..` would compose into a path outside the folder
     * the person picked — and the kernel, once given the all-files permission,
     * would open it.
     *
     * Every part of the id is checked, and the volume is the part that is easy
     * to miss: a document id of `..:data` composes to `/storage/../data`, which
     * `canonicalFile` collapses to `/data` and `isDirectory` then accepts, since
     * it really is a directory. Rejecting only the folder half left exactly the
     * traversal this function exists to refuse.
     *
     * The prefix check at the end is the belt to that braces: whatever the parts
     * spelled, the answer has to still be the folder that was asked for.
     */
    private fun realPathFromTree(uri: Uri): String? = runCatching {
        val docId = DocumentsContract.getTreeDocumentId(uri)
        val type = docId.substringBefore(':', "")
        val rel = docId.substringAfter(':', "")
        if (!isSafeSegment(type) || !isSafeRelPath(rel)) return@runCatching null
        val base = when (type) {
            "primary" -> Environment.getExternalStorageDirectory().absolutePath
            // A removable volume: its document id is the volume's own name
            // (`1234-ABCD`), which is the directory under `/storage`.
            else -> File("/storage", type).path
        }
        val target = if (rel.isEmpty()) base else "$base/$rel"
        val dir = File(target).canonicalFile
        if (!dir.isDirectory) return@runCatching null
        // Still under the volume it claimed to be on. A path that resolved
        // somewhere else is not the folder that was picked, whatever it is.
        val root = File(base).canonicalFile
        val canonical = dir.canonicalPath
        if (canonical != root.canonicalPath && !canonical.startsWith(root.canonicalPath + File.separator)) {
            return@runCatching null
        }
        canonical
    }.getOrNull()

    /** One path component from a document id: a name, never a direction. `.`
     *  and `..` are refused because they are the two spellings of "not a name"
     *  that a filesystem still resolves. */
    private fun isSafeSegment(part: String): Boolean {
        if (part.isEmpty() || part == "." || part == "..") return false
        return !part.contains('/') && !part.contains('\\') && !part.contains('\u0000')
    }

    /** The folder part of a document id: a chain of safe components, each of
     *  which is checked, so a `..` cannot hide behind a later segment. */
    private fun isSafeRelPath(rel: String): Boolean {
        if (rel.isEmpty()) return true
        return rel.split('/').all { isSafeSegment(it) }
    }

    /**
     * The code the camera read, on its way back to the bridge.
     *
     * `data` is null when the person backed out of the scanner, which the bridge
     * reads the same way it reads a refusal: no code. That is the honest answer
     * for both — the page acts on the text, and "why there is none" does not
     * change what it does next.
     */
    private val scanCode = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val text = if (result.resultCode == RESULT_OK) {
            result.data?.getStringExtra(ScanActivity.EXTRA_TEXT)
        } else {
            null
        }
        bridge.deliverScan(text)
    }

    /**
     * Give the page its way into the phone.
     *
     * The bridge object is created per window, holds nothing across a
     * configuration change that the page cannot re-ask for, and is reachable
     * under exactly one name — the one `index.html` and `host.ts` both look for.
     */
    @SuppressLint("AddJavascriptInterface")
    private fun installBridge(target: WebView) {
        bridge = NativeBridge(this, (application as App).kernel) { url ->
            runOnUiThread { openExternally(Uri.parse(url)) }
        }
        bridge.askForFile = {
            // A private key is text, but some providers hand it back as
            // application/octet-stream, and a filter that answered no would show
            // an empty folder. `*/*` plus `setShowHidden` is what actually lets
            // people reach their key.
            pickIdentity.launch(arrayOf("*/*"))
            true
        }
        bridge.onInsets = { currentInsets }
        bridge.askForSave = { name, content ->
            pendingSave = name to content
            runCatching { saveTextFile.launch(name) }
                .onSuccess { }
                .onFailure { pendingSave = null }
                .isSuccess
        }
        bridge.askForShare = { name, content -> shareText(name, content) }
        bridge.askForFolder = { startIn ->
            // The picker opens at its own last location; `startIn` is a hint it
            // may ignore, so a failure to use it is not a failure to ask.
            pickFolderTree.launch(null)
            true
        }
        bridge.onRunning = { running -> runOnUiThread { applyRunning(running) } }
        bridge.filesAccess = { hasAllFilesAccess() }
        bridge.askForFilesAccess = { openAllFilesSettings() }
        bridge.batteryFree = { batteryUnrestricted() }
        bridge.askForBattery = { requestBatteryExemption() }
        bridge.askForScan = {
            // The camera is live from the moment this opens, so the ask is the
            // launch itself; there is no second step to report.
            scanCode.launch(Intent(this, ScanActivity::class.java))
            true
        }
        bridge.onOpenInApp = { url -> openInApp(url) }
        bridge.onHostCall = { id, method, args -> handleHostCall(id, method, args) }
        target.addJavascriptInterface(bridge, "ReasonixNative")
    }

    /**
     * The window's real insets, in CSS pixels, recomputed on every change.
     *
     * Read from the WebView's own window insets rather than from the decor view,
     * and every value is the max of the system bars and the display cutout: a
     * phone with a transparent status bar and a hole-punch camera reports a small
     * or zero bar inset and a real cutout inset, and padding by only the former
     * leaves the interface under the camera.
     */
    private var currentInsets = Insets(0, 0, 0, 0)

    private fun installInsetsListener() {
        ViewCompat.setOnApplyWindowInsetsListener(web) { _, insets ->
            val density = resources.displayMetrics.density.takeIf { it > 0f } ?: 1f
            fun px(value: Int) = Math.round(value / density)
            val system = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            val cutout = insets.getInsets(WindowInsetsCompat.Type.displayCutout())
            val values = Insets(
                left = px(maxOf(system.left, cutout.left)),
                top = px(maxOf(system.top, cutout.top)),
                right = px(maxOf(system.right, cutout.right)),
                bottom = px(maxOf(system.bottom, cutout.bottom)),
            )
            currentInsets = values
            pushInsets(values)
            // The keyboard rides its own channel. It is not a system bar: it
            // comes and goes with focus, and only the composer rises above it.
            //
            // `isVisible` gates the number. An IME inset of zero is not the
            // same as "no keyboard" — a device can report the navigation bar's
            // height on this type while the keyboard is down, and feeding that
            // to the page would lift the composer by a keyboard nobody opened.
            val ime = if (insets.isVisible(WindowInsetsCompat.Type.ime())) {
                px(insets.getInsets(WindowInsetsCompat.Type.ime()).bottom)
            } else {
                0
            }
            pushIme(ime)
            insets
        }
    }

    /**
     * Hand the page the insets it cannot compute for itself.
     *
     * A WebView answers `env(safe-area-inset-*)` with `0` on every Android
     * version — those are an iOS Safari feature — so `mobile.css`'s padding,
     * which is written against them, does nothing here and the first row of the
     * interface sits behind the status bar. Android 15 makes that unconditional
     * by forcing the window edge-to-edge whatever the app asks for, so the
     * numbers have to be pushed in rather than computed in CSS.
     *
     * Pushed on every inset change, not once: the values move when the phone is
     * rotated, when a foldable is unfolded, and when the keyboard opens.
     */
    private fun pushInsets(values: Insets) {
        web.evaluateJavascript(
            "window.__reasonixInsets && window.__reasonixInsets(${values.left},${values.top},${values.right},${values.bottom});",
            null,
        )
    }

    /**
     * Hand the page the keyboard's height, which it cannot reliably measure.
     *
     * The page reads `visualViewport` for this, and that works only while the
     * window stays edge to edge: Android 15 forces that from targetSdk 35, but a
     * WebView told to `adjustResize` shrinks the window instead, and then the
     * visual and layout viewports are equal and the difference the page
     * computes is zero while the keyboard is up. The window insets answer in
     * exactly that case, so the two sources are both sent and the page takes
     * the larger — whichever arrangement the platform picked, the number is the
     * keyboard's real height.
     *
     * Pushed on every inset change, including the one that reports the keyboard
     * going away, so the composer comes back down with it.
     */
    private fun pushIme(bottom: Int) {
        web.evaluateJavascript(
            "window.__reasonixIme && window.__reasonixIme($bottom);",
            null,
        )
    }

    private fun installClients() {
        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val url = request.url
                // The kernel's own page is this app's — and so is a page loaded
                // from a paired computer. After a scan the window *is* that
                // computer's Studio, so its own links belong in here rather than
                // in a browser with no way back.
                val kernelOrigin = (application as App).kernel.origin
                if (kernelOrigin != null && url.toString().startsWith(kernelOrigin)) return false
                if (inAppOrigin != null && originOf(url) == inAppOrigin) return false
                // Anything else belongs in the real browser: a documentation
                // link, a provider's console, a sign-in page. Loading one inside
                // this WebView would strand the person in a window with no
                // address bar.
                if (url.scheme == "http" || url.scheme == "https") {
                    openExternally(url)
                    return true
                }
                // A non-web scheme (mailto:, tel:, an app link) is the system's
                // to route.
                return runCatching { startActivity(Intent(Intent.ACTION_VIEW, url)); true }
                    .getOrDefault(false)
            }

            override fun onPageFinished(view: WebView, url: String) {
                super.onPageFinished(view, url)
                injectHostBridge()
                if (!booted) {
                    booted = true
                    splash.animate().alpha(0f).setDuration(220).withEndAction {
                        splash.visibility = View.GONE
                    }.start()
                }
            }

            override fun onReceivedError(
                view: WebView,
                request: WebResourceRequest,
                error: android.webkit.WebResourceError,
            ) {
                // The main document failing is the kernel being gone, which is
                // worth saying; a subresource failing is noise the page already
                // handles for itself.
                if (request.isForMainFrame && booted) {
                    showFailure(IllegalStateException(getString(R.string.err_kernel_unreachable)))
                }
            }

            override fun onRenderProcessGone(
                view: WebView,
                detail: android.webkit.RenderProcessGoneDetail,
            ): Boolean {
                // The renderer is a separate process and Android reclaims it
                // under pressure. The shell's process — and the kernel, which is
                // its child — is still alive, so the window is rebuilt rather
                // than the app being taken down with its renderer. The kernel is
                // asked for its URL again, which returns the running one.
                rebuildWebView()
                return true
            }
        }

        web.webChromeClient = object : WebChromeClient() {
            override fun onProgressChanged(view: WebView, newProgress: Int) {
                (splash.findViewById<ProgressBar>(R.id.splash_bar))?.progress = newProgress
            }

            /**
             * Hand the page's file input to the system picker.
             *
             * `params.createIntent()` is the intent the page asked for — its
             * `accept` types and its `multiple` flag are already on it — so a
             * photo input opens the gallery and a document input opens the file
             * browser, without this shell deciding which. The callback is kept
             * because the picker is another activity: the answer arrives after
             * this call returns, and only this call's `onReceiveValue` can
             * deliver it.
             */
            override fun onShowFileChooser(
                view: WebView,
                filePathCallback: ValueCallback<Array<Uri>>,
                fileChooserParams: FileChooserParams,
            ): Boolean {
                // A previous callback that never got its answer — a tap while
                // the picker was already open — is released, or the WebView
                // keeps a call that will never complete.
                fileChooserCallback?.onReceiveValue(null)
                fileChooserCallback = filePathCallback
                return try {
                    // Nothing steers where this opens, on purpose.
                    //
                    // 1.2.2 put an `EXTRA_INITIAL_URI` here asking for the
                    // recent collection, as `buildRootUri(…externalstorage…,
                    // "recent")` — a root that provider does not publish (it
                    // has `primary` and `home` only). A root a picker cannot
                    // resolve is not ignored the same way everywhere: on the
                    // device this was reported from it fell through to
                    // 下载内容, so the button opened somewhere worse than the
                    // default it had replaced. What the system picker opens on
                    // already is 「最近」 — measured, not assumed — so the fix
                    // is to ask for nothing and let it.
                    fileChooser.launch(fileChooserParams.createIntent())
                    true
                } catch (_: ActivityNotFoundException) {
                    // No app on the device claims this input's type. Answering
                    // null is a dismissal, which is the honest reading: the
                    // person sees no picker, not a crash.
                    fileChooserCallback = null
                    filePathCallback.onReceiveValue(null)
                    false
                }
            }
        }

        web.setDownloadListener(DownloadListener { url, userAgent, contentDisposition, mimeType, _ ->
            beginDownload(url, userAgent, contentDisposition, mimeType)
        })
    }

    /**
     * A renderer is not restartable, so the only recovery is a new WebView in
     * the same place. The splash comes back for the moment it takes the kernel
     * to answer, which is honest: the page really is being loaded again.
     */
    private fun rebuildWebView() {
        val parent = web.parent as? ViewGroup ?: return
        val index = parent.indexOfChild(web)
        parent.removeView(web)
        web.destroy()

        val fresh = WebView(this)
        web = fresh
        parent.addView(fresh, index, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT,
        ))
        configureWebView()
        // A bridge belongs to one WebView, so a rebuilt renderer needs its own:
        // the old object was reachable only from the destroyed window.
        installBridge(fresh)
        installClients()

        splash.alpha = 1f
        splash.visibility = View.VISIBLE
        // The cookie is still in the jar, so the plain origin is enough and no
        // credential is re-sent.
        (application as App).kernel.plainUrl()?.let { fresh.loadUrl(it) }
    }

    private fun load(origin: String) {
        installClients()
        // The fragment path: the page exchanges the token for an HttpOnly cookie
        // before its first request, exactly as a shared `reasonix web` link does.
        val entry = (application as App).kernel.entryUrl() ?: origin
        web.loadUrl(entry)
    }

    /**
     * Load a URL from a paired computer in this window.
     *
     * Called by the page after a scan. The origin is remembered so the page's own
     * navigation stays in here, and the pairing cookie — which is scoped to that
     * origin and lives in this WebView's jar — keeps working. Handing it to the
     * browser instead would load the same interface with none of its credential.
     */
    private fun openInApp(url: String) {
        val uri = Uri.parse(url)
        val origin = originOf(uri) ?: return
        inAppOrigin = origin
        booted = true
        // The splash has done its job by now — the kernel is up and the page is
        // asking for somewhere else to go — so it must not come back for a
        // navigation that is not a cold start.
        splash.visibility = View.GONE
        web.loadUrl(url)
    }

    /** `scheme://host:port`, the part a cookie is scoped to, or null if the URL
     *  has no host — which makes it something this window will not load. */
    private fun originOf(uri: Uri): String? {
        val scheme = uri.scheme ?: return null
        if (scheme != "http" && scheme != "https") return null
        val host = uri.host ?: return null
        val port = if (uri.port == -1) "" else ":${uri.port}"
        return "$scheme://$host$port"
    }

    private fun showFailure(error: Throwable) {
        splash.alpha = 1f
        splash.visibility = View.VISIBLE
        val bar = splash.findViewById<ProgressBar>(R.id.splash_bar)
        bar?.visibility = View.GONE
        val text = splash.findViewById<android.widget.TextView>(R.id.splash_text)
        text?.text = error.message ?: getString(R.string.err_kernel_unreachable)
        text?.visibility = View.VISIBLE
        val retry = splash.findViewById<android.widget.Button>(R.id.splash_retry)
        retry?.visibility = View.VISIBLE
        retry?.setOnClickListener { recreate() }
    }

    /**
     * Hand text to whatever the device shares with.
     *
     * The share sheet rather than a second save dialog: "where is that file" is
     * the question this answers, and on a phone a document someone wants to keep
     * or send usually goes through a chat app or the mail client rather than a
     * folder they have to navigate to.
     *
     * Sent as plain text with the filename as its subject, which every target
     * understands. An attachment would need a `FileProvider` and a cache copy
     * for the same words to arrive.
     */
    private fun shareText(name: String, content: String): Boolean {
        val send = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_SUBJECT, name)
            putExtra(Intent.EXTRA_TEXT, content)
        }
        return try {
            startActivity(Intent.createChooser(send, getString(R.string.share_title)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            true
        } catch (_: ActivityNotFoundException) {
            // Nothing on the device claims a share. Saying so would be worse than
            // the tap doing nothing, and the page already treats false as "not
            // shared" rather than as an error.
            false
        }
    }

    private fun openExternally(url: Uri) {
        try {
            startActivity(Intent(Intent.ACTION_VIEW, url).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: ActivityNotFoundException) {
            // Nothing on the device claims this scheme; saying so would be worse
            // than the tap doing nothing.
        }
    }

    /**
     * Hold the screen on while a turn is running, and let it go when it is not.
     *
     * `FLAG_KEEP_SCREEN_ON` rather than a wakelock: it is scoped to this window
     * (so the screen is free to sleep the moment the person leaves the app),
     * it cannot leak — there is no acquire/release pair to get wrong — and the
     * system clears it with the window. A turn is the case where the screen
     * going dark costs something: the answer arrives, and the person is looking
     * at it. An idle app is not.
     *
     * Called from the page through `setRunning`, which is the only thing that
     * knows whether the kernel is working.
     */
    private fun applyRunning(running: Boolean) {
        if (!::web.isInitialized) return
        web.keepScreenOn = running
    }

    /** Whether this app may read every file. Below API 30 the concept does not
     *  exist and the answer is yes. */
    private fun hasAllFilesAccess(): Boolean =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) Environment.isExternalStorageManager() else true

    /**
     * The one place Android offers the every-file permission.
     *
     * There is no runtime dialog for it — it is a special access, granted by
     * hand per app — so the only honest implementation is to open the page that
     * holds the switch. A person who does not find it there loses nothing: the
     * app keeps working on the folders it already has.
     */
    private fun openAllFilesSettings() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return
        val intent = Intent(
            Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
            Uri.parse("package:$packageName"),
        )
        runCatching { startActivity(intent) }.recoverCatching {
            // Some ROMs do not route the per-app page; the list is the fallback.
            startActivity(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
        }
    }

    /** Whether this app is exempt from the system's battery optimisation. */
    private fun batteryUnrestricted(): Boolean {
        val power = getSystemService(POWER_SERVICE) as? PowerManager ?: return true
        return power.isIgnoringBatteryOptimizations(packageName)
    }

    /**
     * Ask to be left out of battery optimisation.
     *
     * A running turn lives in a child process under a foreground service, and
     * doze freezes that process anyway — so a task left running with the screen
     * off stops making progress, and comes back as a stalled or disconnected
     * session. The exemption is what a phone needs for work it was told to do
     * in the background, and the dialog is the person's to refuse.
     *
     * `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` rather than the settings
     * list: it is the only form that shows the app's own name and an Allow
     * button, which is what makes the request legible.
     */
    private fun requestBatteryExemption() {
        if (batteryUnrestricted()) return
        val intent = Intent(
            Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
            Uri.parse("package:$packageName"),
        )
        runCatching { requestBattery.launch(intent) }.recoverCatching {
            runCatching { startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)) }
        }
    }

    private val requestBattery = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        // Nothing is reported back to the page. Whether the exemption was granted
        // changes how a *later* background turn behaves, not this render, and the
        // page reads it with `batteryUnrestricted()` when it needs the answer.
    }

    /**
     * A download the page asked for.
     *
     * The name comes from the response's own `Content-Disposition` when it has
     * one, because that is the name the kernel chose for the file; the URL's last
     * segment is the fallback. The picker is the system's, so the person decides
     * where it lands and the app needs no storage permission.
     */
    private fun beginDownload(url: String, userAgent: String?, contentDisposition: String?, mimeType: String?) {
        val name = filenameFrom(contentDisposition, url) ?: getString(R.string.download_default_name)
        pendingDownload = PendingDownload(url, userAgent.orEmpty(), mimeType.orEmpty(), name)
        runCatching { saveDownload.launch(name) }
            .onFailure { pendingDownload = null }
    }

    private fun filenameFrom(contentDisposition: String?, url: String): String? {
        contentDisposition?.let { header ->
            // filename*=UTF-8''… comes first: it is the one that survives a
            // non-ASCII name, which a Chinese filename always is.
            val extended = Regex("filename\\*=UTF-8''([^;]+)", RegexOption.IGNORE_CASE).find(header)
            if (extended != null) {
                return runCatching { java.net.URLDecoder.decode(extended.groupValues[1], "UTF-8") }.getOrNull()
            }
            val plain = Regex("filename=\"?([^\";]+)\"?", RegexOption.IGNORE_CASE).find(header)
            if (plain != null) return plain.groupValues[1]
        }
        return runCatching {
            URL(url).path.substringAfterLast('/').takeIf { it.isNotBlank() }?.let {
                java.net.URLDecoder.decode(it, "UTF-8")
            }
        }.getOrNull()
    }

    // ---- the native browser view ----

    /**
     * The overlay WebView the kernel's browser tools drive.
     *
     * Created lazily on the first showBrowserView call: a WebView costs a
     * renderer process, and most sessions never open the browser panel. Until
     * it exists, the shell's HTTP server answers "not ready" and the page's
     * panel uses its own iframe fallback.
     *
     * The WebViewClient reports load failures back to the page through
     * `onBrowserLoadState`, mapped to Chromium-style error names so the
     * panel's existing error handling works unchanged.
     */
    @SuppressLint("SetJavaScriptEnabled")
    private fun ensureBrowserView(): WebView {
        browserView?.let { return it }
        val wv = WebView(this)
        wv.settings.javaScriptEnabled = true
        wv.settings.domStorageEnabled = true
        wv.visibility = View.GONE
        wv.webViewClient = object : WebViewClient() {
            // A main-frame load that failed still reaches onPageFinished — the
            // WebView considers the attempt finished — so reporting success
            // there would clear the failure onReceivedError had just pushed and
            // the panel would never be told. The flag is per load: cleared when
            // a fresh navigation starts, set when the main frame fails.
            private var mainFrameFailed = false

            override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
                super.onPageStarted(view, url, favicon)
                // A failed load is followed by the browser's own error page,
                // and that page is a load too. Treating it as a fresh
                // navigation cleared the failure the error had just reported,
                // and the panel then drew that error page as if it were the
                // site. It is the product of the failure, not a new attempt.
                if (!url.startsWith("chrome-error:")) {
                    mainFrameFailed = false
                    browserLoadFailure = null
                }
            }

            override fun onPageFinished(view: WebView, url: String) {
                super.onPageFinished(view, url)
                val errorPage = (view.url ?: url).startsWith("chrome-error:")
                if (!mainFrameFailed && !errorPage) pushBrowserLoadState(null, 0, "")
                // The page the caller asked for has arrived, whether it is the
                // site or the browser's error page for it. Either way the
                // navigation is settled and a waiting `/read` should proceed.
                settleBrowserNavigation()
            }

            override fun onReceivedError(
                view: WebView,
                request: WebResourceRequest,
                error: android.webkit.WebResourceError,
            ) {
                if (request.isForMainFrame) {
                    mainFrameFailed = true
                    val reason = mapWebErrorToReason(error.errorCode)
                    browserLoadFailure = request.url.toString() to reason
                    pushBrowserLoadState(
                        request.url.toString(),
                        error.errorCode,
                        reason,
                    )
                    // The read is not released here: at this moment `view.url`
                    // still names the address that failed, and the error page
                    // this failure produces is a load of its own that follows.
                    // `onPageFinished` for that page is where `view.url` reads
                    // `chrome-error:` and the settle belongs.
                }
            }

            // What a page asks the network for, kept for `browser_read
            // what=network`. The interceptor is the only place the WebView says
            // anything about a request, and what it says is the method and the
            // URL: no status, no size. Nothing is returned, so every request
            // continues to be handled by the WebView itself.
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                recordBrowserRequest(request)
                return null
            }
        }
        rootLayout.addView(wv, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT,
        ))
        browserView = wv
        return wv
    }

    /** Show the overlay WebView at the given CSS-pixel rect.
     *
     *  A rect with no area cannot be drawn, and laying the view out at 0×0
     *  leaves it on screen but empty — the page inside reads a zero-size
     *  viewport and stays that way until something gives it a size again. The
     *  page measures the slot and can report an empty one while the panel is
     *  between layouts, so the rect is refused rather than applied; the view
     *  keeps the size it had until a real one arrives. */
    private fun showBrowserView(x: Int, y: Int, w: Int, h: Int) {
        if (w <= 0 || h <= 0) return
        val wv = ensureBrowserView()
        val density = resources.displayMetrics.density.takeIf { it > 0f } ?: 1f
        val lp = FrameLayout.LayoutParams(
            Math.round(w * density),
            Math.round(h * density),
        )
        lp.leftMargin = Math.round(x * density)
        lp.topMargin = Math.round(y * density)
        wv.layoutParams = lp
        wv.visibility = View.VISIBLE
    }

    /**
     * Take a picture of the overlay WebView, hide it, and return the image
     * as a base64 PNG string. "" when there is nothing to capture.
     */
    private fun freezeBrowserView(): String {
        val wv = browserView ?: return ""
        if (wv.visibility != View.VISIBLE) return ""
        return try {
            val width = wv.width.takeIf { it > 0 } ?: return ""
            val height = wv.height.takeIf { it > 0 } ?: return ""
            val bmp = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
            val canvas = Canvas(bmp)
            wv.draw(canvas)
            val baos = ByteArrayOutputStream()
            bmp.compress(Bitmap.CompressFormat.PNG, 85, baos)
            bmp.recycle()
            val b64 = Base64.encodeToString(baos.toByteArray(), Base64.NO_WRAP)
            wv.visibility = View.GONE
            b64
        } catch (_: Exception) {
            wv.visibility = View.GONE
            ""
        }
    }

    /** Load a URL in the overlay WebView. "" when it loads, else why not. */
    private fun navigateBrowserView(address: String): String {
        val trimmed = address.trim()
        if (trimmed.isEmpty()) return "scheme"
        if (trimmed.startsWith("\\\\")) return "network_file"
        val uri = Uri.parse(trimmed)
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") return "scheme"
        ensureBrowserView().loadUrl(trimmed)
        return ""
    }

    /**
     * Handle an async call from the page's `window.reasonixHost` Promise
     * methods. The JS side generated an id and called
     * `ReasonixNative.hostCall(id, method, argsJson)`; this dispatches the
     * work on the UI thread and replies through `evaluateJavascript`.
     */
    private fun handleHostCall(id: String, method: String, args: String) {
        runOnUiThread {
            try {
                when (method) {
                    "showBrowserView" -> {
                        val arr = JSONArray(args)
                        val rect = if (arr.length() > 1) arr.optJSONObject(1) else null
                        showBrowserView(
                            rect?.optInt("x", 0) ?: 0,
                            rect?.optInt("y", 0) ?: 0,
                            rect?.optInt("width", 0) ?: 0,
                            rect?.optInt("height", 0) ?: 0,
                        )
                        replyHost(id, "null")
                    }
                    "hideBrowserView" -> {
                        browserView?.visibility = View.GONE
                        replyHost(id, "null")
                    }
                    "freezeBrowserView" -> {
                        replyHost(id, JSONObject.quote(freezeBrowserView()))
                    }
                    "controlBrowserView" -> {
                        val arr = JSONArray(args)
                        val action = if (arr.length() > 1) arr.optString(1, "") else ""
                        val wv = browserView
                        if (wv != null) {
                            when (action) {
                                "back" -> if (wv.canGoBack()) wv.goBack()
                                "forward" -> if (wv.canGoForward()) wv.goForward()
                                "reload" -> wv.reload()
                                "stop" -> wv.stopLoading()
                            }
                        }
                        replyHost(id, "null")
                    }
                    "navigateBrowserView" -> {
                        val arr = JSONArray(args)
                        val address = if (arr.length() > 1) arr.optString(1, "") else ""
                        replyHost(id, JSONObject.quote(navigateBrowserView(address)))
                    }
                    "trustBrowserCertificate" -> replyHost(id, "false")
                    "answerBrowserLogin" -> replyHost(id, "null")
                    else -> replyHost(id, "null")
                }
            } catch (_: Exception) {
                replyHost(id, "null")
            }
        }
    }

    /** Push a result back to the page's pending Promise. */
    private fun replyHost(id: String, json: String) {
        web.evaluateJavascript(
            "window.__reasonixHostReply && window.__reasonixHostReply(${JSONObject.quote(id)}, ${JSONObject.quote(json)});",
            null,
        )
    }

    /**
     * The lock over [browserNavLatch] and [browserRequests].
     *
     * The WebView's callbacks run on the UI thread while `/read` and
     * `/requests` are served on a connection thread, so these are touched from
     * two threads and neither may see a half-updated view of the other's work.
     */
    private val browserShellLock = Any()

    /** How many requests one page's list keeps. Matches the desktop backend's
     *  bound (`maxRequests`): a page that polls makes thousands, and what an
     *  agent reads is the recent ones. */
    private val maxBrowserRequests = 200

    /**
     * Begin a navigation: the failure and the request list of the page being
     * left stop being this tab's, and a fresh wait is armed for the page being
     * asked for. Called on the UI thread before the new address is loaded, so
     * no callback for the new page can fire ahead of the wait.
     */
    private fun beginBrowserNavigation() {
        synchronized(browserShellLock) {
            browserLoadFailure = null
            browserRequests.clear()
            browserNavLatch = CountDownLatch(1)
        }
    }

    /** The navigation last begun has settled: its page arrived, or its load
     *  failed. Called from the WebView's callbacks on the UI thread; counting
     *  down an already-counted latch is harmless. */
    private fun settleBrowserNavigation() {
        val latch = synchronized(browserShellLock) { browserNavLatch }
        latch?.countDown()
    }

    /**
     * Wait for the navigation last begun to settle, up to [timeoutMs].
     *
     * Called on the connection thread serving `/read`, never the UI thread: the
     * countdown comes from a WebView callback on the UI thread, so waiting there
     * would block the very callback that must release it. A page that never
     * finishes loading falls through on the timeout, and the read then answers
     * about whatever the view is on.
     */
    private fun awaitBrowserNavigation(timeoutMs: Long) {
        val latch = synchronized(browserShellLock) { browserNavLatch } ?: return
        runCatching { latch.await(timeoutMs, TimeUnit.MILLISECONDS) }
    }

    /**
     * Keep what a page asked the network for, for `browser_read what=network`.
     *
     * The request interceptor is the only place the WebView says anything about
     * a request, and what it says is the method and the URL: no status, no
     * size, no content type. That is all the browser tools may report, and the
     * kernel marks every entry as a response it cannot see rather than leaving
     * it looking like one still in flight.
     */
    private fun recordBrowserRequest(request: WebResourceRequest) {
        val url = request.url?.toString() ?: return
        // A data: or blob: URL is page-internal and says nothing about the
        // network; listing them would bury the requests that did travel.
        if (url.startsWith("data:") || url.startsWith("blob:")) return
        val method = request.method ?: "GET"
        synchronized(browserShellLock) {
            browserRequests.add(BrowserRequest(method, url, requestKind(url)))
            while (browserRequests.size > maxBrowserRequests) {
                browserRequests.removeAt(0)
            }
        }
    }

    /**
     * The kind of thing a request asks for, read from the extension of its path.
     *
     * The WebView does not say what a request is for, so this is a guess where
     * the desktop backend has the browser's own answer. It stays conservative:
     * anything unrecognised is "other" rather than given a confident wrong name.
     */
    private fun requestKind(url: String): String {
        val path = runCatching { URL(url).path }.getOrNull().orEmpty()
        return when (path.substringAfterLast('.', "").lowercase()) {
            "html", "htm" -> "document"
            "js", "mjs" -> "script"
            "css" -> "stylesheet"
            "png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "avif" -> "image"
            "woff", "woff2", "ttf", "otf" -> "font"
            "json" -> "fetch"
            "mp4", "webm", "mp3", "ogg", "wav" -> "media"
            "" -> "document"
            else -> "other"
        }
    }

    /**
     * Push a load-state event to the page's `onBrowserLoadState` listeners.
     *
     * A null url means the load succeeded; a non-empty reason means it failed.
     * The shape matches `BrowserLoadState` in host.ts.
     */
    private fun pushBrowserLoadState(url: String?, errorCode: Int, reason: String) {
        val json = if (url != null && reason.isNotEmpty()) {
            """{"targetId":"t1","failure":{"url":${JSONObject.quote(url)},"code":$errorCode,"reason":${JSONObject.quote(reason)}}}"""
        } else {
            """{"targetId":"t1","failure":null}"""
        }
        web.evaluateJavascript(
            "window.__reasonixHostLoadState && window.__reasonixHostLoadState(${JSONObject.quote(json)});",
            null,
        )
    }

    /** Map a WebViewClient error code to a Chromium-style error name. */
    private fun mapWebErrorToReason(code: Int): String = when (code) {
        WebViewClient.ERROR_HOST_LOOKUP -> "ERR_NAME_NOT_RESOLVED"
        WebViewClient.ERROR_CONNECT -> "ERR_CONNECTION_REFUSED"
        WebViewClient.ERROR_TIMEOUT -> "ERR_TIMED_OUT"
        WebViewClient.ERROR_FAILED_SSL_HANDSHAKE -> "ERR_SSL_PROTOCOL_ERROR"
        WebViewClient.ERROR_AUTHENTICATION -> "ERR_HTTP_RESPONSE_CODE_FAILURE"
        WebViewClient.ERROR_IO -> "ERR_CONNECTION_RESET"
        WebViewClient.ERROR_BAD_URL -> "ERR_INVALID_URL"
        WebViewClient.ERROR_UNSUPPORTED_SCHEME -> "ERR_UNKNOWN_URL_SCHEME"
        else -> "ERR_ABORTED"
    }

    /**
     * Inject `window.reasonixHost` into the page.
     *
     * The page's `host.ts` looks for this object to decide whether the shell
     * can draw native browser views. Without it, `drawsBrowserViews()` answers
     * false and the panel falls back to an iframe — which is subject to the
     * target site's X-Frame-Options and fails for most pages worth reading.
     * A WebView is its own browsing context and is not framed by anything.
     *
     * The methods are Promise-based (matching the ElectronBridge shape), but
     * `@JavascriptInterface` is synchronous. The round trip is: JS generates
     * an id, calls `ReasonixNative.hostCall(id, method, argsJson)`, Kotlin
     * does the work asynchronously, then pushes the result back via
     * `evaluateJavascript` calling `window.__reasonixHostReply(id, json)`.
     */
    private fun injectHostBridge() {
        web.evaluateJavascript("""
(function() {
  if (window.reasonixHost) return;
  var pending = {};
  var nextId = 1;
  var loadStateListeners = [];
  var loginListeners = [];
  window.__reasonixHostReply = function(id, jsonStr) {
    var entry = pending[id];
    if (!entry) return;
    delete pending[id];
    try { entry.resolve(JSON.parse(jsonStr)); } catch(e) { entry.resolve(null); }
  };
  window.__reasonixHostLoadState = function(jsonStr) {
    try {
      var state = JSON.parse(jsonStr);
      for (var i = 0; i < loadStateListeners.length; i++) loadStateListeners[i](state);
    } catch(e) {}
  };
  function call(method, args) {
    return new Promise(function(resolve, reject) {
      var id = String(nextId++);
      pending[id] = { resolve: resolve, reject: reject };
      try {
        ReasonixNative.hostCall(id, method, JSON.stringify(args || []));
      } catch(e) {
        delete pending[id];
        reject(e);
      }
    });
  }
  window.reasonixHost = {
    shell: 'electron',
    platform: 'android',
    titleBar: false,
    showBrowserView: function(target, rect) { return call('showBrowserView', [target, rect]); },
    hideBrowserView: function() { return call('hideBrowserView', []); },
    freezeBrowserView: function() { return call('freezeBrowserView', []); },
    controlBrowserView: function(target, action) { return call('controlBrowserView', [target, action]); },
    navigateBrowserView: function(target, address) { return call('navigateBrowserView', [target, address]); },
    onBrowserLoadState: function(listener) {
      loadStateListeners.push(listener);
      return function() {
        var idx = loadStateListeners.indexOf(listener);
        if (idx >= 0) loadStateListeners.splice(idx, 1);
      };
    },
    onBrowserLogin: function(listener) {
      loginListeners.push(listener);
      return function() {
        var idx = loginListeners.indexOf(listener);
        if (idx >= 0) loginListeners.splice(idx, 1);
      };
    },
    answerBrowserLogin: function(id, username, password) { return call('answerBrowserLogin', [id, username, password]); },
    trustBrowserCertificate: function(target) { return call('trustBrowserCertificate', [target]); }
  };
})();
        """.trimIndent(), null)
    }

    override fun onPause() {
        super.onPause()
        // The page keeps its own state; the shell stops the timers that cost
        // battery while it is not being looked at.
        web.onPause()
    }

    override fun onResume() {
        super.onResume()
        web.onResume()
    }

    override fun onDestroy() {
        if (isFinishing) {
            // Swiping the app away means "stop": the kernel is this app's child,
            // and leaving it running with nothing to show it would be a process
            // nobody can reach or stop.
            KernelService.stop(this)
            (application as App).kernel.stop()
            browserShellServer?.stop()
            browserShellServer = null
            browserView?.destroy()
            browserView = null
        } else {
            // A configuration change: the page is rebuilt from the kernel, which
            // is still running.
            web.destroy()
        }
        super.onDestroy()
    }
}
