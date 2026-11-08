package io.reasonix.mobile.app

import android.content.Context
import android.content.res.AssetManager
import org.json.JSONArray
import org.json.JSONObject
import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import java.io.File
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.Locale
import java.util.concurrent.TimeUnit

/** Owns the local Studio host, its private launch handshake, and its bundled page. */
class Kernel(private val context: Context) {

    /** Where everything the kernel owns lives. Nothing here is backed up. */
    private val home = File(context.filesDir, "home")

    /**
     * The folder the agent works in, which is not the folder the kernel lives in.
     *
     * These used to be the same directory, and that is what made the workbench
     * unreadable: the kernel's home holds `config.toml`, `projects/`, `cache/`,
     * `stats/` and the rest of its own bookkeeping, and a workspace pointed at
     * it shows all of that as if the person had put it there. Reported as "why
     * does the default workspace have so many files, the desktop version does
     * not generate these" — the desktop does generate them, in a different
     * directory, because its workspace is whatever folder it was opened on.
     *
     * Split apart, the workbench shows what the agent produced and nothing else.
     * `write_roots` follows the workspace, so the file tools stay confined to
     * the folder the person actually works in rather than to the kernel's own
     * home — which is the stricter answer, and the one the desktop already has.
     */
    private val workspaceDir = File(context.filesDir, "workspace")

    /**
     * The directory the kernel calls `REASONIX_HOME`, for the one caller that
     * has to put a file where the kernel can read it — a private key the person
     * picked from their Documents folder.
     */
    fun homeDir(): File = home

    /** The folder the agent works in. */
    fun workspaceDir(): File = workspaceDir

    /** The built page, unpacked once per version. */
    private val pageDir = File(context.filesDir, "page")

    private val lock = Any()

    @Volatile
    private var process: Process? = null

    /** The port the shell's browser HTTP server listens on, set by the
     *  activity before `start()` is called. Zero means no server is up, and
     *  the kernel leaves its browser tools suspended (the correct degradation
     *  on a phone with no Chromium). */
    @Volatile
    var browserShellPort: Int = 0

    /** The bearer credential for that server, set beside the port. Sent on
     *  every browser call so the server can refuse anything that did not come
     *  from the kernel this shell started — loopback on a phone is reachable
     *  by every other app. Empty when there is no server. */
    @Volatile
    var browserShellToken: String = ""

    /** What the WebView should load, once the kernel has bound a port. */
    @Volatile
    var origin: String? = null
        private set

    /** The host's per-launch credential, held only in memory. */
    @Volatile
    private var token: String? = null

    /** Installed by the native shell before the WebView makes its first request. */
    fun sessionCookie(): String? = token?.let { "reasonix_token=$it; Path=/; HttpOnly; SameSite=Strict" }

    /** The plain origin, for reloads once the cookie is set. */
    fun plainUrl(): String? = origin

    fun isRunning(): Boolean = process?.isAlive == true

    /**
     * Bring the kernel up, or answer the URL of the one already up.
     *
     * Starting is idempotent by intent: the activity and the service both call
     * this, and whichever arrives second joins the running kernel instead of
     * racing it for the port.
     */
    suspend fun start(): Result<String> = withContext(Dispatchers.IO) {
        synchronized(lock) {
            origin?.takeIf { process?.isAlive == true }?.let { return@withContext Result.success(it) }
            var child: Process? = null
            runCatching {
                val binary = kernelBinary()
                publishPage()
                prepareHome()
                val started = launch(binary)
                child = started
                val connection = HostConnection.read(started).get(START_TIMEOUT_MS, TimeUnit.MILLISECONDS)
                awaitServing(started, connection)
                token = connection.token
                origin = connection.origin
                process = started
                connection.origin
            }.onFailure {
                child?.destroyForcibly()
                origin = null
                token = null
                process = null
            }
        }
    }

    /**
     * Stop the kernel.
     *
     * The graceful signal first: the kernel drains its listeners and closes the
     * runtimes it opened, which is what lets a later launch adopt the sessions
     * rather than find them locked. `destroyForcibly` is the second attempt, and
     * only after a wait — a phone that is being shut down should not keep a
     * half-killed agent.
     */
    fun stop() {
        synchronized(lock) {
            val p = process ?: return
            process = null
            origin = null
            token = null
            runCatching {
                p.destroy()
                if (!p.waitFor(3, java.util.concurrent.TimeUnit.SECONDS)) {
                    p.destroyForcibly()
                }
            }
        }
    }

    // ---- the two inputs that ship inside the APK -------------------------

    /**
     * The kernel binary.
     *
     * It is packaged as a native library, so Android has already extracted it to
     * a directory it allows execution from and given it the executable bit —
     * which is the one thing a file under app storage does not get for free.
     */
    private fun kernelBinary(): File {
        val dir = File(context.applicationInfo.nativeLibraryDir)
        val binary = File(dir, "libreasonix.so")
        if (!binary.exists()) {
            throw IllegalStateException(
                "内核未随 APK 安装：${binary.path}\n这是一次构建问题，请重新安装完整 APK。",
            )
        }
        return binary
    }

    /** The entry HTML identifies Vite's content-hashed asset graph, including same-version APK rebuilds. */
    private fun publishPage() {
        val marker = File(pageDir, ".unpacked")
        val entry = context.assets.open("page/index.html").use { it.readBytes() }
        val digest = MessageDigest.getInstance("SHA-256").digest(entry).joinToString("") { "%02x".format(it) }
        val stamp = "${BuildConfig.VERSION_NAME}:${BuildConfig.VERSION_CODE}:$digest"
        if (marker.isFile && marker.readText().trim() == stamp && File(pageDir, "index.html").isFile) return

        pageDir.deleteRecursively()
        pageDir.mkdirs()
        copyAssetTree(context.assets, "page", pageDir)

        if (!File(pageDir, "index.html").isFile) {
            throw IllegalStateException("随 APK 分发的页面不完整：缺少 index.html")
        }
        marker.writeText(stamp)
    }

    /**
     * A directory tree out of assets.
     *
     * `AssetManager.list` on a file answers with an empty array, so a "file" is
     * told from a directory by trying to open it — which is the only signal the
     * API offers and the reason this is not a one-liner.
     */
    private fun copyAssetTree(assets: AssetManager, from: String, to: File) {
        val children = assets.list(from).orEmpty()
        if (children.isEmpty()) {
            assets.open(from).use { input ->
                FileOutputStream(to).use { output -> input.copyTo(output) }
            }
            return
        }
        to.mkdirs()
        for (child in children) {
            copyAssetTree(assets, "$from/$child", File(to, child))
        }
    }

    /**
     * Prepare the kernel's home.
     *
     * `REASONIX_HOME` is what every path in the kernel hangs off — config,
     * sessions, the remote state directory its token file lands in — so pointing
     * it at app storage is what keeps a phone's Reasonix separate from anything
     * else and away from global storage entirely.
     */
    private fun prepareHome() {
        File(home, "remote").mkdirs()
        // The workspace directory not existing yet is what marks the first run
        // after this build split it from the kernel's home. That is the one
        // moment a migration is due, and doing it here means it happens before
        // the kernel is asked for anything.
        val firstSplit = !workspaceDir.exists()
        workspaceDir.mkdirs()
        if (firstSplit) {
            adoptSessionsFromHome()
        }
        // Every launch, not only the first. The rewrite is cheap and
        // idempotent, and an entry that survived the first pass would otherwise
        // survive forever: `firstSplit` is spent the moment the directory
        // exists, so a build that cleaned the list once and missed leaves the
        // home on the sidebar for good. Running it unconditionally costs one
        // small read and makes the whole thing self-correcting.
        forgetHomeFromWorkspaceList()
    }

    /**
     * Drop the kernel's own home from the sidebar's remembered projects.
     *
     * The list is written whenever a pane opens, and until this build the pane
     * opened on the home itself — so an upgraded install carries a "project"
     * that is really the kernel's own directory. Leaving it there would put the
     * person back where they started: one tap on it and the workbench is full
     * of `config.toml`, `cache/` and `projects/` again, which is the complaint
     * this whole change answers. Only that one path is removed; every folder
     * someone added on purpose stays.
     *
     * Parsed rather than pattern-matched, because the file's two shapes — a bare
     * array of paths and an object with `paths` and `launch` — both have to keep
     * whatever else they hold.
     */
    private fun forgetHomeFromWorkspaceList() {
        val file = File(home, "serve-workspaces.json")
        if (!file.isFile) return
        val text = runCatching { file.readText() }.getOrNull() ?: return
        val wrong = runCatching { homeNames() }.getOrNull() ?: return
        val rewritten = runCatching { withoutHome(text, wrong) }.getOrNull() ?: return
        if (!rewritten.dropped) return
        runCatching { file.writeText(rewritten.text) }
            .onSuccess { Log.i(TAG, "dropped the kernel home from the project list") }
            .onFailure { Log.w(TAG, "could not rewrite the project list: ${it.message}") }
    }

    /**
     * Every name of the kernel's home on this device — not just the one this
     * process happens to write.
     *
     * Android shows one directory under two names: `/data/data/<pkg>` and
     * `/data/user/0/<pkg>` are two mounts of the same tree, the same inode
     * either way, and which name a path carries depends on how it was written
     * rather than on where it points. The list is written by the kernel, whose
     * working directory this shell set from a path it built itself; a lookup
     * that compared against only one spelling would silently keep the entry
     * this exists to remove. Both names are compared, so either spelling is
     * recognised.
     */
    private fun homeNames(): Set<String> {
        val path = home.absolutePath
        val names = mutableSetOf(path)
        val user = "/data/user/0/"
        val data = "/data/data/"
        when {
            path.startsWith(user) -> names.add(data + path.removePrefix(user))
            path.startsWith(data) -> names.add(user + path.removePrefix(data))
        }
        return names
    }

    /** What a rewrite produced, and whether it actually removed anything. */
    private class Rewrite(val text: String, val dropped: Boolean)

    /**
     * The list without the home, in whichever shape it arrived, and whether
     * anything was actually dropped from it.
     *
     * The flag matters because `JSONObject` re-serialises: it escapes every
     * `/` and does not preserve key order, so a round trip never reproduces
     * the original bytes. Deciding to write on "the text differs" alone would
     * therefore rewrite the kernel's file on every single launch, log a drop
     * that never happened, and race the kernel for a file it also writes.
     */
    private fun withoutHome(text: String, wrong: Set<String>): Rewrite {
        if (text.trimStart().startsWith("[")) {
            val paths = JSONArray(text)
            val kept = JSONArray()
            var dropped = false
            for (i in 0 until paths.length()) {
                val path = paths.optString(i)
                if (path in wrong) {
                    dropped = true
                    continue
                }
                kept.put(path)
            }
            return if (dropped) Rewrite(kept.toString(), true) else Rewrite(text, false)
        }
        val list = JSONObject(text)
        val kept = JSONArray()
        var dropped = false
        val paths = list.optJSONArray("paths")
        if (paths != null) {
            for (i in 0 until paths.length()) {
                val path = paths.optString(i)
                if (path in wrong) {
                    dropped = true
                    continue
                }
                kept.put(path)
            }
            if (dropped) list.put("paths", kept)
        }
        if (list.optString("launch") in wrong) {
            list.remove("launch")
            dropped = true
        }
        return if (dropped) Rewrite(list.toString(), true) else Rewrite(text, false)
    }

    /**
     * Move the conversations that were filed under the kernel's own home.
     *
     * The transcript store is keyed by the workspace root — one directory per
     * project, under `<state>/projects/<slug>/sessions` — and until this build
     * the root was the home itself. Splitting the two therefore moves every
     * existing conversation to a slug nothing looks under, which reads as "my
     * history is gone" rather than as a reorganisation. So the old directory is
     * renamed onto the new slug once.
     *
     * Renaming the project directory rather than just `sessions/` carries the
     * rest of it too: `memory/` is filed the same way, and a project's memory
     * lost while its transcripts were kept would be a stranger failure than
     * either alone.
     *
     * The slug is the absolute path with its separators flattened, which is
     * what `config.WorkspaceSlug` does on the kernel's side. Only the home's own
     * slug is touched: a slug for some other folder is left exactly where it is,
     * because those are projects someone added on purpose.
     */
    private fun adoptSessionsFromHome() {
        val projects = File(home, "projects")
        if (!projects.isDirectory) return
        val from = projects.resolve(slugOf(home.absolutePath))
        if (!from.isDirectory) return
        val to = projects.resolve(slugOf(workspaceDir.absolutePath))
        // A target already there means a previous run half-finished, or the
        // person opened this folder before. Either way its contents win:
        // merging two transcript directories is not a thing this can do safely,
        // and the old one is still on disk to be looked at.
        if (to.exists()) {
            Log.i(TAG, "${to.name} already exists; leaving ${from.name} in place")
            return
        }
        if (!from.renameTo(to)) {
            Log.w(TAG, "could not move ${from.name} to ${to.name}; history stays under the old slug")
        } else {
            Log.i(TAG, "moved conversations from ${from.name} to ${to.name}")
        }
    }

    /** The kernel's `config.WorkspaceSlug`, for the two paths that matter here. */
    private fun slugOf(path: String): String = path.replace('/', '-').replace('\\', '-').replace(':', '-')

    private fun launch(binary: File): Process {
        val workspace = launchWorkspaceDir()
        val command = listOf(
            binary.absolutePath,
            "-page", pageDir.absolutePath,
            "-studio-version", BuildConfig.VERSION_NAME,
        )
        val builder = ProcessBuilder(command)
        builder.environment().apply {
            put("REASONIX_HOME", home.absolutePath)
            put("HOME", home.absolutePath)
            put("SHELL", "/system/bin/sh")
            // The kernel's own language, for its tool failures, approval
            // prompts and error sentences. Without this it detects from the
            // POSIX locale variables, and Android sets none of them for an
            // app: the kernel then answers in English while the page in front
            // of the person is Chinese, and every sentence added for a Chinese
            // reader was dead text. The tag, not the bare language, so a
            // Traditional Chinese device resolves to zh-TW rather than zh.
            // An explicit `language` in the kernel's config.toml still wins:
            // DetectLanguage applies the configured value over this.
            val languageTag = Locale.getDefault().toLanguageTag()
            if (languageTag.isNotBlank() && languageTag != "und") {
                put("REASONIX_LANG", languageTag)
            }
            // Android has no /tmp. Everything that wants a scratch directory —
            // the shell tool, tool output spooling, sqlite journals — reads this.
            put("TMPDIR", context.cacheDir.absolutePath)
            // The phone's own DNS servers. A CGO-free Go binary cannot read them
            // for itself — Android keeps them in `netd`, and Go looks for a
            // `/etc/resolv.conf` that does not exist — so without this the kernel
            // resolves nothing and every provider probe reports "连接失败" on a
            // device whose browser reaches the same address fine.
            val dns = Dns.servers(context)
            if (dns.isNotBlank()) {
                put("REASONIX_DNS", dns)
                Log.i(TAG, "kernel DNS: $dns (${Dns.describe(context)})")
                if (Dns.looksLikeDot(dns)) {
                    // Private DNS (DNS-over-TLS) is on, and it hands back a
                    // hostname this kernel cannot resolve by definition. Saying
                    // so here is what turns "连不上" into something actionable.
                    Log.w(TAG, "kernel DNS looks like a private-DNS hostname; turn private DNS off or set it to automatic")
                }
            } else {
                Log.w(TAG, "kernel DNS: none reported by the system")
            }
            // The shell's browser server, when one is listening. The kernel
            // reads this to bind its browser tools to the shell's WebView
            // instead of looking for a Chromium binary that does not exist on
            // a phone. When absent (zero), the variable is not set and the
            // kernel keeps the browser tools suspended — the correct
            // degradation for a phone with no engine to drive.
            val shellPort = browserShellPort
            if (shellPort > 0) {
                put("REASONIX_SHELL_BROWSER_PORT", shellPort.toString())
                // The credential the server checks. Set only alongside the
                // port: a token with nothing listening would be a variable the
                // kernel carries for no reason.
                val shellToken = browserShellToken
                if (shellToken.isNotBlank()) {
                    put("REASONIX_SHELL_BROWSER_TOKEN", shellToken)
                }
            }
        }
        // The agent's folder, not the kernel's. `REASONIX_HOME` above is still
        // where the config, the sessions and the transcripts live; this is what
        // the workspace root resolves to, because the kernel reads the working
        // directory for it.
        //
        // The folder the person last opened, when it is still usable — the same
        // answer the desktop host gives (`launchWorkspace`), and the reason a
        // phone session used to work in the app's own `files/workspace` while
        // the sidebar said it was in the shared folder: the launch entry in
        // `serve-workspaces.json` was written and then ignored, and `write_roots`
        // is derived from the working directory, so the file tools were confined
        // to the app's private folder and every write into the shared one was
        // refused. `workspaceDir` is still the answer for a first run and for a
        // folder since deleted, but the choice is never silent — see
        // [launchWorkspaceDir]. `workspace` was resolved above so it could also
        // be named on the command line.
        builder.directory(workspace)
        // `PWD` is what the kernel's own `os.Getwd` prefers: Go uses it whenever
        // it names the same directory as `.`, which is exactly this case, and it
        // is the only way to make the kernel resolve its root to the string this
        // shell chose. Without it `os.Getwd` falls back to the kernel's view of
        // the mount, and on Android `/storage/emulated/0/...` and the path the
        // FUSE layer reports for it need not be spelled the same way. The kernel
        // derives `write_roots` from that root, so the two spellings have to be
        // the same one.
        builder.environment().put("PWD", workspace.absolutePath)
        val process = builder.start()

        // The kernel's own log is the only place a startup failure says why, and
        // a phone has no terminal to read. It is drained on a thread and kept
        // ring-buffered so the About screen can show the last of it.
        Thread({ drain(process) }, "kernel-log").apply { isDaemon = true }.start()
        return process
    }

    /**
     * The folder the person last opened, or null when there is not one.
     *
     * `serve-workspaces.json` is the kernel's own record of the projects the
     * sidebar lists, and its `launch` entry is which of them a session starts
     * in. Both shapes the file has are read — a bare array of paths, and an
     * object with `paths` and `launch` — because both are written in the wild
     * and a reader that only knew one would silently fall back.
     *
     * The entry has to be one of the listed paths, which is the same rule the
     * kernel applies when it reads this file (`workspacelist.Read` blanks a
     * `launch` that is not in `paths`). It matters more here than there: the
     * working directory decides `write_roots`, so whatever this answers is
     * where the agent's file tools may write — and on a phone that permission
     * reaches all of shared storage. A file with a `launch` outside its own
     * list is one this shell did not write, and it gets the listed answer.
     *
     * Null for anything unusable: no file, unreadable JSON, a launch that is
     * not among the listed folders, an entry that has since been deleted, or a
     * path that is now a file. Every one of those means the same thing to the
     * caller — start where this shell always started.
     */
    private fun rememberedLaunchDir(): File? {
        val file = File(home, "serve-workspaces.json")
        if (!file.isFile) return null
        val text = runCatching { file.readText() }.getOrNull() ?: return null
        val launch = runCatching {
            // A bare array is the file's other shape — the one older kernels
            // wrote, and therefore the one a person upgrading is most likely to
            // have. Without this branch `JSONObject(text)` throws on it and the
            // whole function falls back, which is the same mismatch (the
            // sidebar naming one folder, the session working in another) this
            // exists to fix. `withoutHome` below reads both shapes the same way.
            if (text.trimStart().startsWith("[")) {
                val arr = JSONArray(text)
                if (arr.length() == 0) return@runCatching null
                arr.optString(0)
            } else {
                val json = JSONObject(text)
                val paths = json.optJSONArray("paths")
                    ?.let { arr -> (0 until arr.length()).mapNotNull { arr.optString(it).takeIf(String::isNotBlank) } }
                    .orEmpty()
                if (paths.isEmpty()) return@runCatching null
                json.optString("launch").takeIf { it.isNotBlank() && it in paths } ?: paths.first()
            }
        }.getOrNull()?.takeIf { it.isNotBlank() } ?: return null
        val dir = runCatching { File(launch) }.getOrNull() ?: return null
        if (!dir.isDirectory) {
            // A launch entry this process cannot stat is exactly the failure the
            // caller exists to avoid, and falling back silently is what made it
            // take three releases to notice: the session opened in the app's own
            // folder while the sidebar named the shared one. A phone has no
            // terminal, so the log is the only place the reason can be said —
            // and `exists`/`readable` tell apart "deleted" from "not permitted",
            // which need different answers from the person.
            Log.w(
                TAG,
                "launch entry $launch is not a usable directory " +
                    "(exists=${dir.exists()}, readable=${dir.canRead()})",
            )
            return null
        }
        return dir
    }

    /**
     * The folder the kernel should start in.
     *
     * Either the launch entry — the folder the person last opened, which is
     * also the one the sidebar shows — or the app's own folder when there is
     * none. Whichever it is, it is logged, because the two disagreeing is the
     * bug this exists to fix and a silent choice is what let it survive.
     */
    private fun launchWorkspaceDir(): File {
        val remembered = rememberedLaunchDir()
        if (remembered != null) {
            Log.i(TAG, "kernel workspace: ${remembered.absolutePath} (the remembered launch project)")
            return remembered
        }
        Log.i(TAG, "kernel workspace: ${workspaceDir.absolutePath} (no remembered launch project)")
        return workspaceDir
    }

    private val logLines = ArrayDeque<String>()

    private fun drain(process: Process) {
        runCatching {
            process.errorStream.bufferedReader().useLines { lines ->
                lines.forEach { line ->
                    Log.i(TAG, line)
                    synchronized(logLines) {
                        logLines.addLast(line)
                        while (logLines.size > LOG_KEEP) logLines.removeFirst()
                    }
                }
            }
        }
    }

    /** The kernel's tail, newest last, for a bug report or the About screen. */
    fun recentLog(): String = synchronized(logLines) { logLines.joinToString("\n") }

    /**
     * Wait until the bound port answers.
     *
     * A port that is open but not yet serving is the normal first second of a
     * kernel's life; a WebView pointed at it would show a connection error and
     * cache the failure, so the wait belongs here rather than in the page.
     */
    private fun awaitServing(process: Process, connection: HostConnection) {
        val deadline = System.currentTimeMillis() + START_TIMEOUT_MS
        while (System.currentTimeMillis() < deadline) {
            if (!process.isAlive) {
                throw IllegalStateException("内核在开始服务前退出。\n${recentLog().takeLast(2000)}")
            }
            if (probe("${connection.origin}/", connection.token)) return
            Thread.sleep(POLL_MS)
        }
        throw IllegalStateException("内核已监听 ${connection.origin}，但页面没有响应。\n${recentLog().takeLast(2000)}")
    }

    private fun probe(url: String, launchToken: String): Boolean = runCatching {
        (URL(url).openConnection() as HttpURLConnection).run {
            connectTimeout = 1000
            readTimeout = 1000
            requestMethod = "GET"
            setRequestProperty("Cookie", "reasonix_token=$launchToken")
            try {
                responseCode in 200..399
            } finally {
                disconnect()
            }
        }
    }.getOrDefault(false)

    private companion object {
        const val TAG = "ReasonixKernel"
        const val LOG_KEEP = 400
        const val POLL_MS = 120L
        const val START_TIMEOUT_MS = 30_000L
    }
}
