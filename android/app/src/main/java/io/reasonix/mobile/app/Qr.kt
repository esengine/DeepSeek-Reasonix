package io.reasonix.mobile.app

import androidx.camera.core.ImageProxy
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.MultiFormatReader
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.HybridBinarizer

/**
 * Reading a QR code out of one camera frame.
 *
 * ZXing core and nothing else: it is pure Java, so it runs identically on a
 * phone with Google Play Services and on one whose HarmonyOS compatibility
 * layer has none. A decoder that leaned on ML Kit would work on the machine it
 * was written on and fail on the one that needs it.
 *
 * The frame arrives as YUV_420_888, whose Y plane is already the greyscale
 * bitmap ZXing wants — no conversion, no colour maths, just the bytes. That is
 * the whole reason a phone can decode 30 frames a second without warming up.
 */
object Qr {

    private val reader = MultiFormatReader().apply {
        setHints(
            mapOf(
                // Only the form the computer actually renders. Leaving the
                // reader open to every 1D barcode makes each frame slower for
                // a case that cannot occur, and a slower frame is a code that
                // has already left the viewfinder.
                DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
                DecodeHintType.TRY_HARDER to true,
            ),
        )
    }

    /** The code's text, or null when this frame holds none. */
    fun decode(image: ImageProxy): String? {
        val plane = image.planes.firstOrNull() ?: return null
        val buffer = plane.buffer
        val data = ByteArray(buffer.remaining())
        buffer.get(data)
        // `rowStride` is padded to a hardware alignment and is usually wider
        // than the picture. Passing the width as the row length is what tells
        // ZXing where each row actually ends; getting it wrong shears the image
        // and every code reads as garbage.
        val source = PlanarYUVLuminanceSource(
            data,
            plane.rowStride,
            image.height,
            0,
            0,
            image.width,
            image.height,
            false,
        )
        return runCatching {
            reader.decodeWithState(BinaryBitmap(HybridBinarizer(source))).text
        }.getOrNull().also {
            // The reader keeps per-frame state between calls; resetting is what
            // stops a partial match in one frame from corrupting the next.
            reader.reset()
        }
    }
}
