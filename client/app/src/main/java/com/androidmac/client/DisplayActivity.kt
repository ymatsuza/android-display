package com.androidmac.client

import android.os.Bundle
import android.util.Log
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.video.UdpReceiver
import com.androidmac.client.video.VideoDecoder
import com.androidmac.client.video.VideoPacket
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream

class DisplayActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "DisplayActivity"
    }

    private lateinit var surfaceView: SurfaceView
    private var udpReceiver: UdpReceiver? = null
    private var videoDecoder: VideoDecoder? = null
    private var receiveJob: Job? = null
    private var decodeJob: Job? = null

    // Fragment reassembly state
    private var currentSequence: Long = -1
    private val fragments = mutableMapOf<Int, ByteArray>()
    private var expectedFragTotal: Int = 0

    private var streamPort: Int = 0
    private var videoWidth: Int = 0
    private var videoHeight: Int = 0

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_display)

        // Go immersive fullscreen
        enterImmersiveMode()

        streamPort = intent.getIntExtra("streamPort", 0)
        videoWidth = intent.getIntExtra("width", 1920)
        videoHeight = intent.getIntExtra("height", 1080)

        Log.d(TAG, "Display config: ${videoWidth}x${videoHeight}, UDP port=$streamPort")

        surfaceView = findViewById(R.id.surfaceView)
        surfaceView.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                startVideoPipeline(holder)
            }

            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {}

            override fun surfaceDestroyed(holder: SurfaceHolder) {
                stopVideoPipeline()
            }
        })
    }

    private fun enterImmersiveMode() {
        window.insetsController?.let { controller ->
            controller.hide(WindowInsets.Type.systemBars())
            controller.systemBarsBehavior =
                WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
        // Also set legacy flags for broader compatibility
        @Suppress("DEPRECATION")
        window.decorView.systemUiVisibility = (
            View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                or View.SYSTEM_UI_FLAG_FULLSCREEN
                or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                or View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                or View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
            )
    }

    private fun startVideoPipeline(holder: SurfaceHolder) {
        // Initialize decoder
        val decoder = VideoDecoder(videoWidth, videoHeight, holder.surface)
        decoder.start()
        videoDecoder = decoder

        // Start decode loop
        decodeJob = lifecycleScope.launch {
            decoder.decodeLoop()
        }

        // Initialize and start UDP receiver
        val receiver = UdpReceiver(streamPort)
        udpReceiver = receiver

        receiveJob = lifecycleScope.launch {
            receiver.receiveLoop { packet ->
                handlePacket(packet)
            }
        }

        Log.d(TAG, "Video pipeline started")
    }

    private fun handlePacket(packet: VideoPacket) {
        if (packet.fragTotal <= 1) {
            // Single-fragment NAL unit, submit directly
            videoDecoder?.submitNAL(packet.payload)
            return
        }

        // Multi-fragment reassembly
        synchronized(this) {
            if (packet.sequence != currentSequence) {
                // New frame, reset
                currentSequence = packet.sequence
                fragments.clear()
                expectedFragTotal = packet.fragTotal
            }

            fragments[packet.fragIndex] = packet.payload

            if (fragments.size == expectedFragTotal) {
                // All fragments received, reassemble
                val assembled = ByteArrayOutputStream()
                for (i in 0 until expectedFragTotal) {
                    val frag = fragments[i]
                    if (frag != null) {
                        assembled.write(frag)
                    } else {
                        Log.w(TAG, "Missing fragment $i for sequence $currentSequence")
                        fragments.clear()
                        return
                    }
                }
                fragments.clear()
                videoDecoder?.submitNAL(assembled.toByteArray())
            }
        }
    }

    private fun stopVideoPipeline() {
        receiveJob?.cancel()
        decodeJob?.cancel()
        udpReceiver?.close()
        videoDecoder?.stop()
        udpReceiver = null
        videoDecoder = null
        Log.d(TAG, "Video pipeline stopped")
    }

    override fun onDestroy() {
        super.onDestroy()
        stopVideoPipeline()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) {
            enterImmersiveMode()
        }
    }
}
