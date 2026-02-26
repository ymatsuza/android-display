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
import kotlin.math.cos
import kotlin.math.sin

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
    private val touchDispatcher = Dispatchers.IO.limitedParallelism(1)

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
                stopDecoder()
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
        // Initialize decoder (recreated each time the surface changes)
        val decoder = VideoDecoder(videoWidth, videoHeight, holder.surface)
        decoder.start()
        videoDecoder = decoder

        // Start decode loop
        decodeJob = lifecycleScope.launch {
            decoder.decodeLoop()
        }

        // Only set up the UDP receiver and touch pipeline once.
        // On subsequent surface re-creations (e.g. returning from background),
        // the existing UDP stream keeps flowing and the new decoder will pick
        // up from the next keyframe automatically.
        if (udpReceiver != null) {
            Log.d(TAG, "Surface recreated — decoder restarted, waiting for next keyframe")
            return
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

    private var packetCount = 0L
    private var submitCount = 0L

    private fun handlePacket(packet: VideoPacket) {
        packetCount++
        if (packetCount <= 10 || packetCount % 1000 == 0L) {
            Log.d(TAG, "pkt #$packetCount seq=${packet.sequence} ft=0x${String.format("%02x", packet.frameType)} frag=${packet.fragIndex}/${packet.fragTotal} payload=${packet.payload.size}")
        }

        if (packet.fragTotal <= 1) {
            // Single-fragment NAL unit, submit directly with frame type and timestamp
            submitCount++
            if (submitCount <= 10) Log.d(TAG, "submit NAL #$submitCount ft=0x${String.format("%02x", packet.frameType)} size=${packet.payload.size}")
            videoDecoder?.submitNAL(packet.payload, packet.frameType, packet.timestamp)
            return
        }

        // Multi-fragment reassembly
        synchronized(this) {
            if (packet.sequence != currentSequence) {
                // New frame starting — log if previous frame was incomplete
                if (currentSequence >= 0 && fragments.size < expectedFragTotal) {
                    Log.w(TAG, "Dropping incomplete seq=$currentSequence: got ${fragments.size}/$expectedFragTotal frags")
                }
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
                val assembledData = assembled.toByteArray()
                fragments.clear()
                submitCount++
                Log.d(TAG, "submit reassembled NAL #$submitCount ft=0x${String.format("%02x", ft)} size=${assembledData.size} frags=$expectedFragTotal")
                videoDecoder?.submitNAL(assembledData, ft, ts)
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
                MotionEvent.ACTION_CANCEL -> {
                    for (i in 0 until event.pointerCount) {
                        sendTouchEvent(view, event, i, TouchEvent.ACTION_UP)
                    }
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_POINTER_UP -> {
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

        val (tiltX, tiltY) = if (event.getToolType(index) == MotionEvent.TOOL_TYPE_STYLUS) {
            val tilt = event.getAxisValue(MotionEvent.AXIS_TILT, index)
            val orientation = event.getAxisValue(MotionEvent.AXIS_ORIENTATION, index)
            Pair(
                (sin(orientation.toDouble()) * sin(tilt.toDouble())).toFloat(),
                (cos(orientation.toDouble()) * sin(tilt.toDouble())).toFloat()
            )
        } else {
            Pair(0f, 0f)
        }

        val touchEvent = TouchEvent(
            type = touchType,
            action = action,
            x = (event.getX(index) / view.width.toFloat()).coerceIn(0f, 1f),
            y = (event.getY(index) / view.height.toFloat()).coerceIn(0f, 1f),
            pressure = event.getPressure(index).coerceIn(0f, 1f),
            tiltX = tiltX,
            tiltY = tiltY,
            pointerId = event.getPointerId(index),
            timestamp = System.currentTimeMillis()
        )

        lifecycleScope.launch(touchDispatcher) {
            try {
                touchSender?.send(touchEvent)
            } catch (e: Exception) {
                Log.e(TAG, "Touch send error: ${e.message}")
            }
        }
    }

    /** Stop only the decoder (called when Surface is destroyed, e.g. app backgrounded). */
    private fun stopDecoder() {
        decodeJob?.cancel()
        decodeJob = null
        videoDecoder?.stop()
        videoDecoder = null
        Log.d(TAG, "Decoder stopped (surface destroyed)")
    }

    /** Tear down everything — called once when the Activity is destroyed. */
    private fun stopAllPipelines() {
        stopDecoder()
        receiveJob?.cancel()
        receiveJob = null
        udpReceiver?.close()
        udpReceiver = null
        touchSender?.disconnect()
        touchSender = null
        Log.d(TAG, "All pipelines stopped")
    }

    override fun onDestroy() {
        super.onDestroy()
        stopAllPipelines()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) {
            enterImmersiveMode()
        }
    }
}
