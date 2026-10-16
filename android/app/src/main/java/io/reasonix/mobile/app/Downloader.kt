package io.reasonix.mobile.app

import android.content.Context
import android.net.Uri
import android.webkit.CookieManager
import android.widget.Toast
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.FileNotFoundException
import java.net.HttpURLConnection
import java.net.URL

/**
 * Putting a file the page produced onto the phone's disk.
 *
 * A WebView has no downloads shelf and cannot write to the system's document
 * picker itself, so the two halves are joined here: the page asks, the picker
 * answers with a `content://` the app has been granted, and this copies the
 * bytes across.
 *
 * The request carries the WebView's own cookie, read out of the jar the page
 * authenticated in. That is the only reason this is not a plain `openStream`:
 * the kernel's download route is behind the session the page established, and a
 * shell that fetched without it would get a 403 and no explanation.
 */
object Downloader {

    suspend fun run(
        context: Context,
        url: String,
        userAgent: String,
        mime: String,
        name: String,
        target: Uri,
    ): Result<Unit> = withContext(Dispatchers.IO) {
        runCatching {
            val connection = (URL(url).openConnection() as HttpURLConnection).apply {
                connectTimeout = 15_000
                readTimeout = 60_000
                requestMethod = "GET"
                if (userAgent.isNotBlank()) setRequestProperty("User-Agent", userAgent)
                // The download route answers with the same session cookie the
                // page is using; without it the kernel refuses the request.
                cookieFor(url)?.let { setRequestProperty("Cookie", it) }
            }
            try {
                val code = connection.responseCode
                if (code !in 200..299) {
                    throw IllegalStateException(
                        context.getString(R.string.download_failed_status, code),
                    )
                }
                val resolver = context.contentResolver
                val output = resolver.openOutputStream(target, "w")
                    ?: throw FileNotFoundException(target.toString())
                output.use { out ->
                    connection.inputStream.use { input -> input.copyTo(out) }
                }
                // `copyTo` answers with a byte count; what the caller wants to
                // know is that there were no bytes missing, not how many.
                Unit
            } finally {
                connection.disconnect()
            }
        }
    }

    /**
     * The cookie the page authenticated with, if any.
     *
     * `CookieManager` is the WebView's own store, so this asks the exact jar the
     * request will be checked against rather than a copy this shell kept.
     */
    private fun cookieFor(url: String): String? =
        runCatching { CookieManager.getInstance().getCookie(url) }.getOrNull()?.takeIf { it.isNotBlank() }

    /** A short receipt for a tap that otherwise does nothing visible. */
    fun report(context: Context, name: String, result: Result<Unit>) {
        val message = result.fold(
            onSuccess = { context.getString(R.string.download_saved, name) },
            onFailure = { failure ->
                context.getString(R.string.download_failed, failure.message ?: "")
            },
        )
        Toast.makeText(context, message, Toast.LENGTH_LONG).show()
    }
}
