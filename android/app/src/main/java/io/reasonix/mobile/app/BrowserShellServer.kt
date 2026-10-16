package io.reasonix.mobile.app

import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.util.Log
import android.webkit.WebView
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/**
 * The kernel's browser, as a loopback HTTP server.
 *
 * The Go kernel (`internal/platform/browser/shell.go`) drives a WebView by
 * POSTing JSON to a handful of paths on 127.0.0.1. This class is the other end
 * of that conversation: it accepts connections on a random port, parses the
 * minimal HTTP the kernel sends, and translates each path into an operation on
 * the overlay WebView.
 *
 * The server is deliberately tiny — no connection pooling, no keep-alive, no
 * chunked encoding — because the kernel is its only client and sends one
 * request per connection. A `ServerSocket` accept loop on a daemon thread is
 * the whole transport; each request is handled synchronously on its own thread.
 *
 * WebView operations must run on the UI thread. Each handler posts to the main
 * looper and waits on a latch, with a timeout that bounds the worst case (a
 * page that hangs the JS thread) to something the kernel's own 30-second client
 * timeout will not outlast.
 *
 * Memory note on `/screenshot`: a full-page `WebView.draw` into an ARGB_8888
 * bitmap at device resolution can reach tens of MB for a long page. The bitmap
 * is downsampled by a factor of 2 in each dimension before PNG encoding, which
 * keeps the peak allocation under ~10 MB for a typical 1080p viewport. The
 * original is recycled immediately after the downsample.
 */
/** One thing a page asked the network for, as the WebView's request
 *  interceptor reports it. The interceptor is handed the method and the URL
 *  and nothing about the response, so a caller may show exactly this much and
 *  must not read it as a completed exchange. */
data class BrowserRequest(val method: String, val url: String, val kind: String)

class BrowserShellServer(
    /** The overlay WebView, when one already exists. The routes that act on an
     *  open page — back, forward, reload, close — use this: with no page there
     *  is nothing to act on, and creating one to do nothing to it would start a
     *  renderer nobody asked for. */
    private val existingView: () -> WebView?,
    /** The overlay WebView, created on first use. The routes that drive a page
     *  use this, because the kernel can open one before the panel has ever been
     *  shown: the page's own panel asks for the view when it is displayed, and
     *  an agent's `browser_open` can arrive first. Requiring the view to exist
     *  already answered every such call with "browser view not ready", which
     *  reads as a broken engine rather than as a view that was not created yet.
     *  Called on the UI thread. */
    private val ensureView: () -> WebView,
    /** Why the page in the view is the browser's own error page rather than a
     *  site, as `(url, chromium error)`; null when the last load succeeded.
     *  `/read` asks this, because extracting `innerText` from the error page
     *  returns the browser's own "this page could not be loaded" notice, which
     *  the kernel would hand to the model as though the site had said it. */
    private val loadFailure: () -> Pair<String, String>? = { null },
    /** Begin a navigation: forget the load failure recorded for the page being
     *  left, drop its request list, and start the wait a following `/read`
     *  joins. Called on the UI thread, before the new address is loaded, so the
     *  wait is in place before any callback for the new page can fire. */
    private val startNavigation: () -> Unit = {},
    /** What the page currently in the view has asked the network for, oldest
     *  first. The WebView reports a request's method and URL to its interceptor
     *  but nothing about the response, so that is all a caller may show. */
    private val requests: () -> List<BrowserRequest> = { emptyList() },
    /** Wait for the navigation last asked for to settle — the page arrived, or
     *  its load failed — up to the given milliseconds. Called on the request's
     *  own thread, never the UI thread: the settle is signalled from the UI
     *  thread, so waiting on it there would deadlock. `/load` starts a
     *  navigation and returns before the page has been visited; without this a
     *  `/read` right behind it still sees the URL of the page before, and a
     *  failure recorded for that page was handed back as the new one's answer. */
    private val awaitNavigation: (Long) -> Unit = {},
) {

    companion object {
        private const val TAG = "BrowserShellServer"

        /** How long a UI-thread operation may take before the handler gives up.
         *  The kernel's client timeout is 30 s; staying under it means the kernel
         *  sees a clean error rather than a dropped connection. */
        private const val UI_TIMEOUT_MS = 25_000L

        /** The maximum text `/read` returns. Matches the kernel's `maxReadText`
         *  (400 << 10). The shell truncates here and says so, rather than letting
         *  the kernel truncate silently. */
        private const val MAX_READ_TEXT = 400 * 1024

        /** Screenshot downsample factor. 2 means half width and half height. */
        private const val SCREENSHOT_SCALE = 2

        /** The address the kernel dials. Both ends must agree on the family:
         *  see [start] for what goes wrong when they do not. */
        private const val LOOPBACK_V4 = "127.0.0.1"

        /** How long `/read` waits for a navigation it follows to settle before
         *  reading whatever the view is on. A page that never finishes loading
         *  must not hold the tool call open to the kernel's own timeout. */
        private const val NAV_SETTLE_MS = 10_000L

        /** How long `/read` then waits for the page's own script to answer.
         *  `NAV_SETTLE_MS + READ_SCRIPT_MS` has to stay under [UI_TIMEOUT_MS],
         *  and that under the kernel's 30-second client timeout: a read that
         *  waited out both budgets would outlast the caller, which would see a
         *  dropped connection instead of this shell's answer. 10 + 15 = 25 s. */
        private const val READ_SCRIPT_MS = 15_000L
    }

    @Volatile
    private var serverSocket: ServerSocket? = null

    /** The port the server is listening on. Valid after [start] returns. */
    @Volatile
    var port: Int = 0
        private set

    /**
     * The bearer credential every request must carry. Minted by the activity
     * and handed to the kernel through the child's environment, so the server
     * answers only the process this shell started.
     *
     * A phone's loopback is not a boundary: any other app may connect to
     * 127.0.0.1, and the routes here run script in and read text out of
     * whatever page is loaded. Binding to `getLoopbackAddress()` keeps other
     * machines out but not other apps, so the credential is what makes "only
     * the kernel this shell started" true.
     */
    @Volatile
    var token: String = ""
        private set

    private val mainHandler = Handler(Looper.getMainLooper())

    /**
     * Start listening on 127.0.0.1 with an ephemeral port. The port is available
     * via [port] immediately after this returns, and the credential the kernel
     * must present via [token].
     */
    fun start() {
        token = newToken()
        // The loopback address is spelled out as IPv4, not taken from
        // `InetAddress.getLoopbackAddress()`. That call is allowed to answer
        // either family and Android's libcore answers IPv6 (`::1`), while the
        // kernel's client is hardcoded to `http://127.0.0.1:<port>`
        // (`internal/assembly/boot/browser.go`). A socket bound to `::1` does
        // not answer a dial to `127.0.0.1`, so every browser call failed with
        // "connection refused" while the port was bound and reported — the
        // failure looked like a server that never started. Both ends have to
        // name the same family, and IPv4 is the one the kernel names.
        val ss = ServerSocket(0, 16, java.net.InetAddress.getByName(LOOPBACK_V4))
        serverSocket = ss
        port = ss.localPort
        Log.i(TAG, "browser shell server listening on $LOOPBACK_V4:$port")

        Thread({
            while (!ss.isClosed) {
                try {
                    val client = ss.accept()
                    Thread({ handleClient(client, token) }, "browser-shell-conn").apply { isDaemon = true }.start()
                } catch (_: SocketException) {
                    // Server was closed; exit the loop.
                }
            }
        }, "browser-shell-accept").apply { isDaemon = true }.start()
    }

    fun stop() {
        runCatching { serverSocket?.close() }
        serverSocket = null
    }

    /** A fresh 256-bit credential, hex-encoded, for one launch of the server. */
    private fun newToken(): String {
        val bytes = ByteArray(32)
        java.security.SecureRandom().nextBytes(bytes)
        return bytes.joinToString("") { "%02x".format(it) }
    }

    // ---- HTTP plumbing --------------------------------------------------------

    private fun handleClient(client: Socket, expected: String) {
        try {
            client.soTimeout = 30_000
            val input = BufferedInputStream(client.getInputStream())
            val output = client.getOutputStream()

            // Parse the request line.
            val requestLine = readLine(input) ?: return
            val parts = requestLine.split(" ", limit = 3)
            if (parts.size < 2) return
            val method = parts[0]
            val path = parts[1].substringBefore('?')

            // Read headers.
            var contentLength = 0
            var bearer: String? = null
            while (true) {
                val header = readLine(input) ?: break
                if (header.isEmpty()) break
                val lower = header.lowercase()
                if (lower.startsWith("content-length:")) {
                    contentLength = header.substringAfter(':').trim().toIntOrNull() ?: 0
                } else if (lower.startsWith("authorization:")) {
                    bearer = header.substringAfter(':').trim().removePrefix("Bearer ").trim()
                }
            }

            // Nothing behind this server is safe to hand out on the strength of
            // an address: the port is on loopback, which any other app on the
            // phone can dial, and the routes run script in and read text out of
            // the loaded page. The credential the kernel was started with is the
            // only thing that says a caller is that kernel. Every route is
            // refused without it, /health included — a probe needs no exemption.
            if (expected.isEmpty() || bearer != expected) {
                writeResponse(output, 401, """{"error":"unauthorized"}""")
                return
            }

            // Read body.
            val body = if (contentLength > 0) {
                val buf = ByteArray(contentLength)
                var read = 0
                while (read < contentLength) {
                    val n = input.read(buf, read, contentLength - read)
                    if (n == -1) break
                    read += n
                }
                String(buf, 0, read, Charsets.UTF_8)
            } else ""

            // Route.
            val (status, responseBody) = route(method, path, body)
            writeResponse(output, status, responseBody)
        } catch (e: Exception) {
            Log.w(TAG, "connection error: ${e.message}")
        } finally {
            runCatching { client.close() }
        }
    }

    private fun route(method: String, path: String, body: String): Pair<Int, String> {
        return when {
            method == "GET" && path == "/health" -> 200 to """{"ok":true}"""
            method == "POST" && path == "/load" -> handleLoad(body)
            method == "POST" && path == "/close" -> handleClose()
            method == "POST" && path == "/read" -> handleRead()
            method == "POST" && path == "/screenshot" -> handleScreenshot()
            method == "POST" && path == "/click" -> handleClick(body)
            method == "POST" && path == "/eval" -> handleEval(body)
            method == "POST" && path == "/back" -> handleBack()
            method == "POST" && path == "/forward" -> handleForward()
            method == "POST" && path == "/reload" -> handleReload()
            method == "POST" && path == "/requests" -> handleRequests()
            else -> 404 to """{"error":"not found"}"""
        }
    }

    private fun writeResponse(out: OutputStream, status: Int, body: String) {
        val reason = when (status) {
            200 -> "OK"
            404 -> "Not Found"
            500 -> "Internal Server Error"
            else -> "Error"
        }
        val bytes = body.toByteArray(Charsets.UTF_8)
        val header = "HTTP/1.1 $status $reason\r\n" +
            "Content-Type: application/json\r\n" +
            "Content-Length: ${bytes.size}\r\n" +
            "Connection: close\r\n" +
            "\r\n"
        out.write(header.toByteArray(Charsets.UTF_8))
        out.write(bytes)
        out.flush()
    }

    private fun readLine(input: BufferedInputStream): String? {
        val sb = StringBuilder()
        while (true) {
            val b = input.read()
            if (b == -1) return if (sb.isEmpty()) null else sb.toString()
            if (b == '\r'.code) {
                val next = input.read()
                if (next == '\n'.code) return sb.toString()
                sb.append(b.toChar())
                if (next != -1) sb.append(next.toChar())
                continue
            }
            if (b == '\n'.code) return sb.toString()
            sb.append(b.toChar())
        }
    }

    // ---- endpoint handlers ----------------------------------------------------

    /** POST /load {"url":"..."} → {"url":"...","title":"..."} */
    private fun handleLoad(body: String): Pair<Int, String> {
        val json = runCatching { JSONObject(body) }.getOrNull()
            ?: return 500 to """{"error":"invalid request body"}"""
        val url = json.optString("url", "")
        if (url.isEmpty()) return 500 to """{"error":"missing url"}"""

        val result = AtomicReference("")
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = ensureView()
            // The failure recorded for the page being left stops being this
            // tab's, and the wait a following `/read` joins is armed before the
            // new address is loaded, so no callback for the new page can fire
            // ahead of it. `/load` answers before the page arrives, which is
            // what left the old failure to be read back as the new page's.
            startNavigation()
            wv.loadUrl(url)
            // The title and final URL are not available synchronously after
            // loadUrl; answer with what was requested. The kernel stores this
            // as the tab's URL and reads the real one on the next /read.
            result.set(JSONObject().apply {
                put("url", url)
                put("title", "")
            }.toString())
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout waiting for UI thread"}"""
        }
        return 200 to result.get()
    }

    /** POST /close {} → 200 */
    private fun handleClose(): Pair<Int, String> {
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = existingView()
            if (wv != null) {
                wv.loadUrl("about:blank")
                wv.clearHistory()
            }
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        return 200 to "{}"
    }

    /**
     * POST /read {} → {"title":"...","url":"...","text":"..."}
     *
     * Injects a script that extracts readable text from the page, preferring
     * `<article>` or `<main>` and falling back to `body.innerText`. The result
     * is truncated to 400 KB (matching the kernel's `maxReadText`); when
     * truncated, a note is appended so the model knows the text was cut.
     */
    private fun handleRead(): Pair<Int, String> {
        // The page the caller just asked for has not necessarily been visited
        // yet: `/load` returns as soon as it has started the navigation. Waiting
        // here, off the UI thread, is what makes this read answer about that
        // page instead of the one before it.
        awaitNavigation(NAV_SETTLE_MS)
        val result = AtomicReference("")
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = ensureView()
            // A page that did not load is not content. Whether the view is on
            // the browser's own error page is read from the view itself rather
            // than from a remembered failure: a failure recorded for the page
            // before a new navigation would otherwise be handed back as this
            // page's answer, long after the page the caller asked for had
            // loaded. The remembered failure is still what names the address
            // that failed and why, since the error page's own URL does not.
            if ((wv.url ?: "").startsWith("chrome-error:")) {
                val (url, reason) = loadFailure() ?: ((wv.url ?: "") to "ERR_FAILED")
                result.set(JSONObject().apply {
                    put("url", url)
                    put("title", "")
                    put("reason", reason)
                    put("text", "")
                }.toString())
                latch.countDown()
                return@post
            }
            // The extraction script: prefer article/main for content pages,
            // fall back to body. innerText preserves visual line breaks. It
            // also stamps a ref on every element a person could act on, so the
            // kernel can name one in an act step the way the desktop backend
            // names an accessibility node. A ref already on an element is kept,
            // so it stays the same across snapshots of one document; a fresh
            // document starts the numbering over, and the kernel only accepts a
            // ref from the snapshot it last read.
            val script = """
                (function(){
                    var seq = window.__rxRefSeq || 0;
                    var used = {};
                    var marked = document.querySelectorAll('[data-rx-ref]');
                    for (var i = 0; i < marked.length; i++) { used[marked[i].getAttribute('data-rx-ref')] = true; }
                    function roleOf(el) {
                        var tag = el.tagName.toLowerCase();
                        var role = (el.getAttribute('role') || '').toLowerCase();
                        if (role) return role;
                        if (tag === 'a') return 'link';
                        if (tag === 'button') return 'button';
                        if (tag === 'select') return 'combobox';
                        if (tag === 'textarea') return 'textbox';
                        if (tag === 'input') {
                            var t = (el.getAttribute('type') || 'text').toLowerCase();
                            if (t === 'checkbox' || t === 'radio' || t === 'submit' || t === 'button' || t === 'reset') return t;
                            return 'textbox';
                        }
                        return 'generic';
                    }
                    function nameOf(el) {
                        var n = el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.getAttribute('name') || '';
                        if (!n) { n = (el.innerText || el.textContent || '').trim(); }
                        return n.replace(/\s+/g, ' ').slice(0, 160);
                    }
                    var sel = 'a[href],button,input,select,textarea,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[contenteditable=true]';
                    var els = document.querySelectorAll(sel);
                    var elements = [];
                    for (var j = 0; j < els.length; j++) {
                        var el = els[j];
                        var ref = el.getAttribute('data-rx-ref');
                        if (!ref) {
                            do { seq++; ref = 'e' + seq; } while (used[ref]);
                            el.setAttribute('data-rx-ref', ref);
                            used[ref] = true;
                        }
                        var raw = (el.value != null && String(el.value) !== '') ? String(el.value).slice(0, 160) : '';
                        elements.push({ref: ref, role: roleOf(el), name: nameOf(el), value: raw});
                    }
                    window.__rxRefSeq = seq;
                    var el2 = document.querySelector('article') || document.querySelector('main') || document.body;
                    var text = el2 ? (el2.innerText || '') : '';
                    var title = document.title || '';
                    var url = location.href;
                    return JSON.stringify({title: title, url: url, text: text, elements: elements});
                })()
            """.trimIndent()
            wv.evaluateJavascript(script) { value ->
                // evaluateJavascript returns a JSON-encoded string (the result
                // is double-encoded). Strip the outer quotes.
                val parsed = parseJsResult(value)
                if (parsed != null) {
                    try {
                        val obj = JSONObject(parsed)
                        var text = obj.optString("text", "")
                        if (text.length > MAX_READ_TEXT) {
                            text = text.substring(0, MAX_READ_TEXT) +
                                "\n\n[text truncated at 400 KB]"
                        }
                        result.set(JSONObject().apply {
                            put("title", obj.optString("title", ""))
                            put("url", obj.optString("url", ""))
                            put("text", text)
                            put("elements", obj.optJSONArray("elements") ?: JSONArray())
                        }.toString())
                    } catch (e: Exception) {
                        result.set(JSONObject().apply {
                            put("title", "")
                            put("url", wv.url ?: "")
                            put("text", parsed.take(MAX_READ_TEXT))
                        }.toString())
                    }
                } else {
                    result.set(JSONObject().apply {
                        put("title", "")
                        put("url", wv.url ?: "")
                        put("text", "")
                    }.toString())
                }
                latch.countDown()
            }
        }
        if (!latch.await(READ_SCRIPT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout waiting for page script"}"""
        }
        return 200 to result.get()
    }

    /**
     * POST /screenshot {} → {"data":"<base64>","mime":"image/png"}
     *
     * Draws the WebView into a bitmap, downsamples by SCREENSHOT_SCALE to keep
     * memory bounded, and encodes as PNG → base64. For a 1080×2400 viewport the
     * full bitmap is ~10 MB; the downsampled one is ~2.5 MB. The full bitmap is
     * recycled immediately after scaling.
     */
    private fun handleScreenshot(): Pair<Int, String> {
        val result = AtomicReference("")
        val failed = AtomicBoolean(false)
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = ensureView()
            try {
                val width = wv.width.takeIf { it > 0 } ?: 1080
                val height = wv.height.takeIf { it > 0 } ?: 1920
                val bmp = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
                val canvas = Canvas(bmp)
                wv.draw(canvas)

                // Downsample to bound the PNG size and the base64 string.
                val scaled = Bitmap.createScaledBitmap(
                    bmp,
                    width / SCREENSHOT_SCALE,
                    height / SCREENSHOT_SCALE,
                    true,
                )
                if (scaled !== bmp) bmp.recycle()

                val baos = ByteArrayOutputStream()
                scaled.compress(Bitmap.CompressFormat.PNG, 85, baos)
                scaled.recycle()
                val b64 = Base64.encodeToString(baos.toByteArray(), Base64.NO_WRAP)
                result.set(JSONObject().apply {
                    put("data", b64)
                    put("mime", "image/png")
                }.toString())
            } catch (e: Exception) {
                // A 200 with an error body would be read by the kernel as a
                // screenshot whose data is empty, and it would build a `data:`
                // URI out of nothing. The status is what tells it this failed.
                result.set("""{"error":"screenshot failed: ${e.message}"}""")
                failed.set(true)
            }
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        if (failed.get()) return 500 to result.get()
        return 200 to result.get()
    }

    /** POST /click {"selector":"..."} → {"ok":true|false} */
    private fun handleClick(body: String): Pair<Int, String> {
        val json = runCatching { JSONObject(body) }.getOrNull()
            ?: return 500 to """{"error":"invalid request body"}"""
        val selector = json.optString("selector", "")
        if (selector.isEmpty()) return 500 to """{"error":"missing selector"}"""

        val result = AtomicReference("")
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = ensureView()
            // Escape the selector for embedding in a JS string.
            val escaped = selector.replace("\\", "\\\\").replace("'", "\\'")
            val script = """
                (function(){
                    try {
                        var el = document.querySelector('$escaped');
                        if (!el) return JSON.stringify({ok: false});
                        el.click();
                        return JSON.stringify({ok: true});
                    } catch(e) {
                        return JSON.stringify({ok: false});
                    }
                })()
            """.trimIndent()
            wv.evaluateJavascript(script) { value ->
                val parsed = parseJsResult(value)
                result.set(parsed ?: """{"ok":false}""")
                latch.countDown()
            }
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        return 200 to result.get()
    }

    /** POST /eval {"script":"..."} → {"result":"..."} */
    private fun handleEval(body: String): Pair<Int, String> {
        val json = runCatching { JSONObject(body) }.getOrNull()
            ?: return 500 to """{"error":"invalid request body"}"""
        val script = json.optString("script", "")
        if (script.isEmpty()) return 500 to """{"error":"missing script"}"""

        val result = AtomicReference("")
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = ensureView()
            wv.evaluateJavascript(script) { value ->
                // The kernel expects a plain string in "result". The value from
                // evaluateJavascript is JSON-encoded; unwrap it.
                val unwrapped = parseJsResult(value) ?: ""
                result.set(JSONObject().apply {
                    put("result", unwrapped)
                }.toString())
                latch.countDown()
            }
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout waiting for eval"}"""
        }
        return 200 to result.get()
    }

    /** POST /back → 200 */
    private fun handleBack(): Pair<Int, String> {
        val latch = CountDownLatch(1)
        mainHandler.post {
            existingView()?.let { if (it.canGoBack()) it.goBack() }
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        return 200 to "{}"
    }

    /** POST /forward → 200 */
    private fun handleForward(): Pair<Int, String> {
        val latch = CountDownLatch(1)
        mainHandler.post {
            existingView()?.let { if (it.canGoForward()) it.goForward() }
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        return 200 to "{}"
    }

    /** POST /reload → 200 */
    private fun handleReload(): Pair<Int, String> {
        val latch = CountDownLatch(1)
        mainHandler.post {
            val wv = existingView()
            // A view sitting on the browser's own error page is not on the site,
            // and reloading it would retry the error page itself. The address
            // that failed is loaded again instead — which is the page a caller
            // means by "reload" — read before `startNavigation` clears it.
            val failed = if (wv != null && (wv.url ?: "").startsWith("chrome-error:"))
                loadFailure()?.first else null
            startNavigation()
            if (wv != null) {
                if (failed != null) wv.loadUrl(failed) else wv.reload()
            }
            latch.countDown()
        }
        if (!latch.await(UI_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return 500 to """{"error":"timeout"}"""
        }
        return 200 to "{}"
    }

    /** POST /requests {} → {"requests":[{"method","url","kind"},…]} */
    private fun handleRequests(): Pair<Int, String> {
        val arr = JSONArray()
        for (r in requests()) {
            arr.put(JSONObject().apply {
                put("method", r.method)
                put("url", r.url)
                put("kind", r.kind)
            })
        }
        return 200 to JSONObject().apply { put("requests", arr) }.toString()
    }

    // ---- helpers ----------------------------------------------------------------

    /**
     * Unwrap the result of `evaluateJavascript`. The callback receives a
     * JSON-encoded string: a JS string "hello" arrives as `"\"hello\""`, a
     * number 42 as `"42"`, null as `"null"`. Strip the outer JSON encoding to
     * get the raw value.
     */
    private fun parseJsResult(value: String?): String? {
        if (value == null || value == "null") return null
        // The value is a JSON literal. If it is a JSON string, decode it.
        return try {
            if (value.startsWith("\"") && value.endsWith("\"")) {
                JSONObject().apply { }.let {
                    // Use JSONObject's string decoding: wrap in an object.
                    JSONObject("""{"v":$value}""").optString("v", "")
                }
            } else {
                value
            }
        } catch (_: Exception) {
            value
        }
    }
}