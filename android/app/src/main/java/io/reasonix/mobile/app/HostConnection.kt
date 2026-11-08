package io.reasonix.mobile.app

import org.json.JSONObject
import java.net.URI
import java.util.concurrent.CompletableFuture

internal class HostConnection(val origin: String, val token: String) {
    companion object {
        fun read(process: Process): CompletableFuture<HostConnection> {
            val ready = CompletableFuture<HostConnection>()
            Thread({
                try {
                    process.inputStream.bufferedReader().use { reader ->
                        val line = reader.readLine() ?: error("应用内核未返回启动信息")
                        val handshake = JSONObject(line)
                        val origin = handshake.getString("origin")
                        val token = handshake.getString("token")
                        val uri = URI(origin)
                        require(handshake.getInt("version") == 1 &&
                            uri.scheme == "http" && uri.host == "127.0.0.1" &&
                            uri.port in 1..65535 && uri.rawPath.orEmpty().isEmpty() &&
                            uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
                            token.matches(Regex("[0-9a-f]{64}"))) { "应用内核启动信息无效" }
                        ready.complete(HostConnection(origin, token))
                        while (reader.readLine() != null) { }
                    }
                } catch (_: Exception) {
                    ready.completeExceptionally(IllegalStateException("应用内核未返回有效的本地连接"))
                }
            }, "kernel-handshake").apply { isDaemon = true }.start()
            return ready
        }
    }
}
