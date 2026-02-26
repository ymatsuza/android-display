package com.androidmac.client.video

import android.media.MediaCodec
import android.media.MediaFormat
import android.util.Log
import android.view.Surface
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.util.concurrent.ConcurrentLinkedQueue

/**
 * Holds a NAL unit together with its frame type and presentation timestamp
 * so the decoder can set the correct MediaCodec flags.
 */
data class NALEntry(val data: ByteArray, val frameType: Byte, val timestamp: Long)

class VideoDecoder(
    private val width: Int,
    private val height: Int,
    private val surface: Surface
) {
    private var codec: MediaCodec? = null
    private val nalQueue = ConcurrentLinkedQueue<NALEntry>()

    companion object {
        private const val TAG = "VideoDecoder"
        private const val MIME = "video/avc"
        private const val TIMEOUT_US = 10_000L

        // Frame type constants matching the Go stream package
        const val FRAME_TYPE_SPS: Byte = 0x10
        const val FRAME_TYPE_PPS: Byte = 0x11
    }

    fun start() {
        val format = MediaFormat.createVideoFormat(MIME, width, height)
        codec = MediaCodec.createDecoderByType(MIME).apply {
            configure(format, surface, null, 0)
            start()
        }
        Log.d(TAG, "Decoder started: ${width}x${height}")
    }

    /**
     * Submit a NAL unit for decoding. The frameType is used to determine
     * whether BUFFER_FLAG_CODEC_CONFIG should be set (for SPS/PPS).
     */
    fun submitNAL(data: ByteArray, frameType: Byte, timestamp: Long) {
        nalQueue.offer(NALEntry(data, frameType, timestamp))
    }

    suspend fun decodeLoop() = withContext(Dispatchers.IO) {
        val codec = codec ?: return@withContext
        val bufferInfo = MediaCodec.BufferInfo()

        while (isActive) {
            // Feed input
            val entry = nalQueue.poll()
            if (entry != null) {
                val idx = codec.dequeueInputBuffer(TIMEOUT_US)
                if (idx >= 0) {
                    val buf = codec.getInputBuffer(idx)!!
                    buf.clear()
                    buf.put(entry.data)

                    // C3+I1: Set BUFFER_FLAG_CODEC_CONFIG for SPS/PPS NAL units
                    val flags = when (entry.frameType) {
                        FRAME_TYPE_SPS, FRAME_TYPE_PPS -> MediaCodec.BUFFER_FLAG_CODEC_CONFIG
                        else -> 0
                    }

                    codec.queueInputBuffer(idx, 0, entry.data.size, entry.timestamp, flags)
                }
            }

            // Drain output
            val outIdx = codec.dequeueOutputBuffer(bufferInfo, TIMEOUT_US)
            if (outIdx >= 0) {
                codec.releaseOutputBuffer(outIdx, true)
            }
        }
    }

    fun stop() {
        try {
            codec?.stop()
            codec?.release()
        } catch (_: Exception) {
        }
        codec = null
        Log.d(TAG, "Decoder stopped")
    }
}
