package io.reasonix.mobile.app

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.view.Gravity
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.TextView
import android.widget.Button
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import java.util.concurrent.Executors

/**
 * The camera, held open just long enough to read one pairing code.
 *
 * A phone that wants to drive a computer cannot type the code that computer
 * shows: it is a base64 fragment inside a URL, and the whole point of showing it
 * as a QR code is that nobody reads one aloud. So this is the way in — the same
 * gesture the computer's own dialog asks for, from the other side.
 *
 * The decode is ZXing rather than ML Kit on purpose. ML Kit needs Google Play
 * Services, and this app installs on phones whose HarmonyOS compatibility layer
 * has none: a GMS-backed scanner would work on a developer's phone and fail on
 * the device that needs it. ZXing core is pure Java, so the scanner behaves the
 * same on both.
 *
 * It returns the first code it reads and finishes. A pairing code is
 * single-use and short-lived, so "keep scanning after a hit" is not a feature —
 * it is a way to spend a code that a later frame would have discarded.
 */
class ScanActivity : AppCompatActivity() {

    private lateinit var preview: PreviewView
    private lateinit var hint: TextView
    private val executor = Executors.newSingleThreadExecutor()

    /** Set once a code is found, so the frames in flight behind it do not each
     *  try to finish the activity. */
    @Volatile
    private var settled = false

    private val requestCamera = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) start() else refuse()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val root = FrameLayout(this)
        preview = PreviewView(this).apply {
            layoutParams = FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT,
            )
            // The frames are analysed, not composed: filling the view and
            // cropping is right here and would be wrong for a camera app.
            scaleType = PreviewView.ScaleType.FILL_CENTER
        }
        hint = TextView(this).apply {
            text = getString(R.string.scan_hint)
            setTextColor(0xFFFFFFFF.toInt())
            textSize = 14f
            gravity = Gravity.CENTER
            val pad = (16 * resources.displayMetrics.density).toInt()
            setPadding(pad, pad, pad, pad)
            // Readable over whatever the camera is pointing at, which is the
            // one background a design system cannot choose.
            setBackgroundColor(0x99000000.toInt())
            layoutParams = FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
                Gravity.BOTTOM,
            )
        }
        root.addView(preview)
        root.addView(hint)
        root.addView(Button(this).apply {
            text = getString(R.string.scan_close)
            setOnClickListener { setResult(RESULT_CANCELED); finish() }
            layoutParams = FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.TOP or Gravity.START)
        })
        ViewCompat.setOnApplyWindowInsetsListener(root) { view, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        setContentView(root)

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) ==
            PackageManager.PERMISSION_GRANTED
        ) {
            start()
        } else {
            requestCamera.launch(Manifest.permission.CAMERA)
        }
    }

    private fun refuse(message: Int = R.string.scan_denied) {
        Toast.makeText(this, message, Toast.LENGTH_LONG).show()
        setResult(RESULT_CANCELED)
        finish()
    }

    private fun start() {
        val providerFuture = ProcessCameraProvider.getInstance(this)
        providerFuture.addListener({
            val provider = runCatching { providerFuture.get() }.getOrNull() ?: return@addListener refuse(R.string.scan_unavailable)
            val previewUseCase = Preview.Builder().build().also { it.setSurfaceProvider(preview.surfaceProvider) }
            val analysis = ImageAnalysis.Builder()
                // Only the newest frame matters: a stale one describes where the
                // camera was, and a code that has already left the viewfinder
                // must not win over one that is in it now.
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
                .also { it.setAnalyzer(executor, ::analyse) }
            runCatching {
                provider.unbindAll()
                provider.bindToLifecycle(this, CameraSelector.DEFAULT_BACK_CAMERA, previewUseCase, analysis)
            }.onFailure { refuse(R.string.scan_unavailable) }
        }, ContextCompat.getMainExecutor(this))
    }

    private fun analyse(image: ImageProxy) {
        try {
            if (settled) return
            val text = Qr.decode(image) ?: return
            if (settled) return
            settled = true
            runOnUiThread {
                setResult(RESULT_OK, Intent().putExtra(EXTRA_TEXT, text))
                finish()
            }
        } finally {
            image.close()
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        executor.shutdown()
    }

    companion object {
        const val EXTRA_TEXT = "text"
    }
}
