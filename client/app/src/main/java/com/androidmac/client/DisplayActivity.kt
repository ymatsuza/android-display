package com.androidmac.client

import android.annotation.SuppressLint
import android.os.Bundle
import android.util.Log
import android.view.MotionEvent
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.control.ControlClient
import com.androidmac.client.touch.TouchEvent
import com.androidmac.client.touch.TouchSender
import com.androidmac.client.video.UdpReceiver
import com.androidmac.client.video.VideoDecoder
import com.androidmac.client.video.VideoPacket
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream

class DisplayActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "DisplayActivity"

        /**
         * Set by MainActivity before launching DisplayActivity so the control
         * connection can be reused to send ClientReady.
         */
        @Volatile
        var pendingControlClient: ControlClient? = null
    }

    private lateinit var surfaceView: SurfaceView
    private var udpReceiver: UdpReceiver? = null
    private var videoDecoder: VideoDecoder? = null
    private var receiveJob: Job? = null
    private var decodeJob: Job? = null

    // Fragment reassembly state
    private var currentSequence: Long = -1
    private var currentFrameType: Byte = 0
    private var currentTimestamp: Long = 0
    private val fragments = mutableMapOf<Int, ByteArray>()
    private var expectedFragTotal: Int = 0

    private var videoWidth: Int = 0
    private var videoHeight: Int = 0

    // Shared ControlClient passed from MainActivity
    private var controlClient: ControlClient? = null

    // Touch support
    private var touchSender: TouchSender? = null
    private var touchPort: Int = 0
    private var serverHost: String = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_display)

        // Go immersive fullscreen
        enterImmersiveMode()

        videoWidth = intent.getIntExtra("width", 1920)
        videoHeight = intent.getIntExtra("height", 1080)
        touchPort = intent.getIntExtra("touchPort", 0)
        serverHost = intent.getStringExtra("serverHost") ?: ""

        // Pick up the ControlClient set by MainActivity
        controlClient = pendingControlClient
        pendingControlClient = null

        Log.d(TAG, "Display config: ${videoWidth}x${videoHeight}")

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

        // C1: Bind UDP to port 0 (auto-assign) to avoid port conflicts,
        // then send ClientReady with the actual port back to the server.
        val receiver = UdpReceiver(0)
        val actualPort = receiver.bind()
        udpReceiver = receiver

        Log.d(TAG, "UDP bound to port $actualPort")

        // Send the actual UDP port to the server via the TCP control connection
        lifecycleScope.launch {
            try {
                controlClient?.sendReady(actualPort)
                Log.d(TAG, "Sent ClientReady with UDP port $actualPort")
            } catch (e: Exception) {
                Log.e(TAG, "Failed to send ClientReady: ${e.message}", e)
            }
        }

        receiveJob = lifecycleScope.launch {
            receiver.receiveLoop { packet ->
                handlePacket(packet)
            }
        }

        Log.d(TAG, "Video pipeline started")

        // Start touch pipeline after video is ready
        startTouchPipeline()
    }

    private fun handlePacket(packet: VideoPacket) {
        if (packet.fragTotal <= 1) {
            // Single-fragment NAL unit, submit directly with frame type and timestamp
            videoDecoder?.submitNAL(packet.payload, packet.frameType, packet.timestamp)
            return
        }

        // Multi-fragment reassembly
        synchronized(this) {
            if (packet.sequence != currentSequence) {
                // New frame, reset
                currentSequence = packet.sequence
                currentFrameType = packet.frameType
                currentTimestamp = packet.timestamp
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
                val ft = currentFrameType
                val ts = currentTimestamp
                fragments.clear()
                videoDecoder?.submitNAL(assembled.toByteArray(), ft, ts)
            }
        }
    }

    private fun startTouchPipeline() {
        if (touchPort <= 0 || serverHost.isEmpty()) {
            Log.w(TAG, "Touch not available: port=$touchPort host=$serverHost")
            return
        }

        val sender = TouchSender()
        touchSender = sender

        lifecycleScope.launch {
            try {
                sender.connect(serverHost, touchPort)
                Log.d(TAG, "Touch connected to $serverHost:$touchPort")
                setupTouchHandling()
            } catch (e: Exception) {
                Log.e(TAG, "Touch connection failed: ${e.message}", e)
            }
        }
    }

    @SuppressLint("ClickableViewAccessibility")
    private fun setupTouchHandling() {
        surfaceView.setOnTouchListener { view, event ->
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN, MotionEvent.ACTION_POINTER_DOWN -> {
                    val idx = event.actionIndex
                    sendTouchEvent(view, event, idx, TouchEvent.ACTION_DOWN)
                }
                MotionEvent.ACTION_MOVE -> {
                    for (i in 0 until event.pointerCount) {
                        sendTouchEvent(view, event, i, TouchEvent.ACTION_MOVE)
                    }
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_POINTER_UP, MotionEvent.ACTION_CANCEL -> {
                    val idx = event.actionIndex
                    sendTouchEvent(view, event, idx, TouchEvent.ACTION_UP)
                }
            }
            true
        }
    }

    private fun sendTouchEvent(view: View, event: MotionEvent, index: Int, action: Byte) {
        val touchType = when (event.getToolType(index)) {
            MotionEvent.TOOL_TYPE_STYLUS -> TouchEvent.TYPE_PEN
            else -> TouchEvent.TYPE_FINGER
        }

        val touchEvent = TouchEvent(
            type = touchType,
            action = action,
            x = (event.getX(index) / view.width.toFloat()).coerceIn(0f, 1f),
            y = (event.getY(index) / view.height.toFloat()).coerceIn(0f, 1f),
            pressure = event.getPressure(index).coerceIn(0f, 1f),
            pointerId = event.getPointerId(index),
            timestamp = System.currentTimeMillis()
        )

        lifecycleScope.launch(Dispatchers.IO) {
            try {
                touchSender?.send(touchEvent)
            } catch (e: Exception) {
                Log.e(TAG, "Touch send error: ${e.message}")
            }
        }
    }

    private fun stopVideoPipeline() {
        receiveJob?.cancel()
        decodeJob?.cancel()
        udpReceiver?.close()
        videoDecoder?.stop()
        touchSender?.disconnect()
        udpReceiver = null
        videoDecoder = null
        touchSender = null
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
