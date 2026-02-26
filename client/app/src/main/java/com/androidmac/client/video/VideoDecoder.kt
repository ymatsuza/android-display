package com.androidmac.client.video

import android.media.MediaCodec
import android.media.MediaFormat
import android.util.Log
import android.view.Surface
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.util.concurrent.ConcurrentLinkedQueue

class VideoDecoder(
    private val width: Int,
    private val height: Int,
    private val surface: Surface
) {
    private var codec: MediaCodec? = null
    private val nalQueue = ConcurrentLinkedQueue<ByteArray>()

    companion object {
        private const val TAG = "VideoDecoder"
        private const val MIME = "video/avc"
        private const val TIMEOUT_US = 10_000L
    }

    fun start() {
        val format = MediaFormat.createVideoFormat(MIME, width, height)
        codec = MediaCodec.createDecoderByType(MIME).apply {
            configure(format, surface, null, 0)
            start()
        }
        Log.d(TAG, "Decoder started: ${width}x${height}")
    }

    fun submitNAL(data: ByteArray) {
        nalQueue.offer(data)
    }

    suspend fun decodeLoop() = withContext(Dispatchers.IO) {
        val codec = codec ?: return@withContext
        val bufferInfo = MediaCodec.BufferInfo()

        while (isActive) {
            // Feed input
            val nal = nalQueue.poll()
            if (nal != null) {
                val idx = codec.dequeueInputBuffer(TIMEOUT_US)
                if (idx >= 0) {
                    val buf = codec.getInputBuffer(idx)!!
                    buf.clear()
                    buf.put(nal)
                    codec.queueInputBuffer(idx, 0, nal.size, 0, 0)
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
