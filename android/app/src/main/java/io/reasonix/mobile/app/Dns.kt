package io.reasonix.mobile.app

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.os.Build

/**
 * The phone's DNS servers, as the kernel needs them.
 *
 * The kernel is a CGO-free Go binary, and Go's own source explains why that
 * matters:
 *
 * ```
 * // DNS requests don't work on Android, so prefer the cgo resolver.
 * // Issue #10714.
 * case "android":
 * ```
 *
 * A normal Android program resolves names through `getaddrinfo`, which reaches
 * the `netd` daemon and gets the servers of the network that is actually up.
 * A static binary has no cgo resolver, so Go reads `/etc/resolv.conf` instead —
 * and Android has no such file. Go then falls back to `127.0.0.1:53`, where
 * nothing listens, and every lookup fails: the setup screen reports
 * "连接失败" for every provider, including the official ones, on a phone whose
 * browser reaches them fine.
 *
 * So they are read here and handed to the kernel through `REASONIX_DNS`. This is
 * the same source the system itself uses, so a VPN, a private DNS hostname, or a
 * network that changed since boot is all reflected without this code knowing
 * anything about them.
 */
object Dns {

    /**
     * The active network's servers, comma separated, or "" when there is none.
     *
     * Read from the *active* network rather than from every link, because a phone
     * commonly has several at once — Wi-Fi, mobile data, a VPN — and the servers
     * of a network that is not carrying traffic are the wrong ones to ask.
     */
    fun servers(context: Context): String {
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: return ""
        val properties: LinkProperties = runCatching { manager.activeNetwork }.getOrNull()
            ?.let { network -> runCatching { manager.getLinkProperties(network) }.getOrNull() }
            ?: return ""
        return properties.dnsServers
            .mapNotNull { address -> address.hostAddress }
            .filter { it.isNotBlank() }
            .filter { routable(it) }
            .sortedBy { preference(it) }
            .joinToString(",")
    }

    /**
     * Whether an address can actually carry a query.
     *
     * A link-local address (fe80::/10) without its zone cannot: the packet has
     * no interface to leave by, so it is dropped, nothing answers, and the
     * lookup hangs until the kernel's own timeout expires — which is reported as
     * a timeout rather than as a failure to resolve, and so reads like the
     * network is slow instead of like the address is unusable.
     *
     * Android reports the zone as a `%wlan0` suffix on `hostAddress`, and the
     * address is only usable with it kept. Nothing here trims it: an earlier
     * version called `substringBefore('%')`, which turned a usable link-local
     * server into an unroutable one.
     */
    private fun routable(host: String): Boolean =
        !(host.startsWith("fe80:", ignoreCase = true) && !host.contains('%'))

    /**
     * IPv4 first, then IPv6.
     *
     * A phone commonly reports both, and its IPv6 entry is often a router's
     * link-local address — reachable on one network and not the next. The
     * kernel tries the servers in the order it is given them, and stops at the
     * first that answers; putting the address family that shares the network's
     * own numbering first means its timeout is spent on a server that works.
     */
    private fun preference(host: String): Int = if (host.contains(':')) 1 else 0

    /**
     * The private-DNS setting, for the one case the address list cannot answer.
     *
     * Since Android 9 a phone may use DNS-over-TLS, and then `dnsServers` holds a
     * name rather than an address — a name this kernel cannot resolve *because*
     * resolving it is the thing that is broken. There is no way for the shell to
     * do that lookup for it, so this reports the situation instead of handing
     * over an unusable value: the caller says so in the log, and the person can
     * turn private DNS off or set it to automatic.
     */
    fun looksLikeDot(servers: String): Boolean =
        servers.isNotBlank() && servers.split(',').any { part ->
            val host = part.trim()
            host.isNotEmpty() && host.none { it == ':' } && !host.all { it.isDigit() || it == '.' }
        }

    /** Whether the system requires a VPN or private DNS, which changes nothing
     *  here but is worth saying in a bug report. */
    fun describe(context: Context): String {
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: return "no connectivity service"
        val network = runCatching { manager.activeNetwork }.getOrNull() ?: return "no active network"
        val properties = runCatching { manager.getLinkProperties(network) }.getOrNull()
            ?: return "no link properties"
        // The DHCP server arrived in API 30, not 23: asked for below that it is a
        // NoSuchMethodError on a device this app supports (minSdk 26), and the
        // answer is only ever read out of this string by a person debugging.
        val dhcp = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            properties.dhcpServerAddress?.hostAddress
        } else {
            null
        }
        return "iface=${properties.interfaceName} dns=${properties.dnsServers.joinToString(",") { it.hostAddress ?: "?" }}" +
            (dhcp?.let { " dhcp=$it" } ?: "")
    }
}
