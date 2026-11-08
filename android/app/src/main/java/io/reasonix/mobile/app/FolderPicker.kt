package io.reasonix.mobile.app

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.DocumentsContract
import android.provider.Settings
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import java.io.File

internal class FolderPicker(private val activity: AppCompatActivity, private val deliver: (String?) -> Unit) {
    private var startIn = ""
    private val tree = activity.registerForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        val path = uri?.let(::realPath)
        if (uri != null && path == null) message(R.string.workspace_unavailable)
        deliver(path)
    }
    private val settings = activity.registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        afterPermission()
    }
    private val legacy = activity.registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        afterPermission()
    }

    fun hasAccess(): Boolean = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        Environment.isExternalStorageManager()
    } else {
        ContextCompat.checkSelfPermission(activity, Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED
    }

    fun pick(path: String): Boolean {
        startIn = path
        if (hasAccess()) {
            launchTree()
        } else {
            AlertDialog.Builder(activity)
                .setTitle(R.string.wsperm_title)
                .setMessage(R.string.wsperm_body)
                .setPositiveButton(R.string.wsperm_go) { _, _ -> requestAccess() }
                .setNegativeButton(R.string.wsperm_later) { _, _ -> deliver(null) }
                .setOnCancelListener { deliver(null) }
                .show()
        }
        return true
    }

    fun requestAccess() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            legacy.launch(arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE))
            return
        }
        runCatching {
            settings.launch(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:${activity.packageName}")))
        }.recoverCatching {
            settings.launch(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
        }.onFailure {
            message(R.string.workspace_permission_denied)
            deliver(null)
        }
    }

    private fun afterPermission() {
        if (hasAccess()) launchTree() else {
            message(R.string.workspace_permission_denied)
            deliver(null)
        }
    }

    private fun launchTree() {
        val root = Environment.getExternalStorageDirectory().absolutePath
        val relative = if (startIn.startsWith("$root/")) startIn.removePrefix("$root/") else "Documents"
        val initial = DocumentsContract.buildDocumentUri(AUTHORITY, "primary:$relative")
        runCatching { tree.launch(initial) }.onFailure {
            message(R.string.workspace_unavailable)
            deliver(null)
        }
    }

    private fun message(resource: Int) = Toast.makeText(activity, resource, Toast.LENGTH_LONG).show()

    private fun realPath(uri: Uri): String? = runCatching {
        if (uri.authority != AUTHORITY) return@runCatching null
        val document = DocumentsContract.getTreeDocumentId(uri)
        val volume = document.substringBefore(':', "")
        val relative = document.substringAfter(':', "")
        if (!safeSegment(volume) || (relative.isNotEmpty() && !relative.split('/').all(::safeSegment))) return@runCatching null
        val root = if (volume == "primary") Environment.getExternalStorageDirectory() else File("/storage", volume)
        val target = File(root, relative).canonicalFile
        val base = root.canonicalPath
        if (target.path != base && !target.path.startsWith(base + File.separator)) return@runCatching null
        target.path.takeIf { target.isDirectory && target.canRead() && target.canWrite() }
    }.getOrNull()

    private fun safeSegment(part: String): Boolean = part.isNotEmpty() && part != "." && part != ".." &&
        part.none { it == '/' || it == '\\' || it == '\u0000' }

    private companion object {
        const val AUTHORITY = "com.android.externalstorage.documents"
    }
}
