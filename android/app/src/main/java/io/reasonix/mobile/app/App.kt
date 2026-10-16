package io.reasonix.mobile.app

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context

/**
 * The process-wide facts this app needs.
 *
 * The kernel is one child process, so there is exactly one of it no matter how
 * many activities exist: it is held here, lazily, and the activity and the
 * service both reach the same instance. Nothing else is stored — the theme, the
 * language and every preference belong to the page and live in its localStorage.
 */
class App : Application() {

    /**
     * The kernel, created on first use.
     *
     * Lazy rather than eager so that merely launching the process to answer a
     * notification does not unpack a 15MB page.
     */
    val kernel: Kernel by lazy { Kernel(this) }

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    /**
     * A channel the person can mute without muting the app, and one they can
     * find: an agent that runs for minutes on a phone has to say so somewhere
     * the system will not kill, and a notification is the only place Android
     * offers. `IMPORTANCE_MIN` because the notification is a receipt, not an
     * interruption — the work it reports is on another machine.
     */
    private fun createChannel() {
        // No SDK_INT guard: minSdk is 26, which is the release NotificationChannel
        // arrived in, so every device this app installs on has it.
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (manager.getNotificationChannel(CHANNEL_KERNEL) != null) return
        val channel = NotificationChannel(
            CHANNEL_KERNEL,
            getString(R.string.channel_kernel),
            NotificationManager.IMPORTANCE_MIN,
        ).apply {
            description = getString(R.string.channel_kernel_desc)
            setShowBadge(false)
            enableVibration(false)
            setSound(null, null)
        }
        manager.createNotificationChannel(channel)
    }

    companion object {
        const val CHANNEL_KERNEL = "reasonix-kernel"
    }
}
