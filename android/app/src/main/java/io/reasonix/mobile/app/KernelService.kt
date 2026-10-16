package io.reasonix.mobile.app

import android.Manifest
import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * Keeps the kernel alive while the app is not on screen.
 *
 * The reason this exists at all: an agent turn that takes minutes is a normal
 * turn, and Android reclaims a backgrounded app's child process within seconds
 * of the screen going off. Without the service, pocketing the phone would kill
 * a running task — the one failure mode that makes a phone a bad client for
 * something that works on its own.
 *
 * A foreground service needs a notification the person can see, which is also
 * the honest thing to show: the kernel is running and what it is doing lives on
 * another machine.
 */
class KernelService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Promoted before `start()`: a service that starts work first and
        // notifies after has a window in which the system may kill it, and the
        // work it would have been protecting is exactly what is being started.
        startForegroundNotification()

        val kernel = (application as App).kernel
        scope.launch {
            kernel.start().onFailure { failure ->
                // A kernel that will not start is a fact the person needs, and a
                // notification that only ever said "running" would be a lie.
                notify(SERVICE_NOTICE, getString(R.string.notify_failed_title), failure.message.orEmpty())
                stopSelf()
            }
        }
        // Sticky, so a kernel killed under memory pressure comes back when there
        // is room — which is what a person who left a task running expects.
        return START_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun startForegroundNotification() {
        val notification = buildNotification(
            getString(R.string.notify_running_title),
            getString(R.string.notify_running_body),
        )
        // `dataSync` matches the manifest's declaration. On 34+ the type is
        // mandatory and a mismatch throws, so it is named in both places.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            ServiceCompat.startForeground(
                this,
                SERVICE_NOTICE,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
            )
        } else {
            startForeground(SERVICE_NOTICE, notification)
        }
    }

    private fun buildNotification(title: String, body: String): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            },
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, App.CHANNEL_KERNEL)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(body)
            .setContentIntent(open)
            // The notification is a receipt, not an alarm; swiping it away
            // leaves the kernel running, which is what the person meant by
            // swiping it.
            .setOngoing(false)
            .setSilent(true)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .build()
    }

    private fun notify(id: Int, title: String, body: String) {
        // The notification permission is granted at runtime from Android 13 and
        // the person can refuse it. Without this check the call throws
        // SecurityException and takes the service down with it — the opposite of
        // what a notification that could not be shown should do. Below 13 the
        // permission does not exist and posting always succeeds.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            return
        }
        runCatching {
            NotificationManagerCompat.from(this).notify(id, buildNotification(title, body))
        }
    }

    companion object {
        private const val SERVICE_NOTICE = 1

        /** Idempotent: called on every start of the activity, and by the boot
         *  path, and safe to repeat. */
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, KernelService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, KernelService::class.java))
        }
    }
}
