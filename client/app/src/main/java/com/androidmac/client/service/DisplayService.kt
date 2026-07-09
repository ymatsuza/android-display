package com.androidmac.client.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Binder
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.core.app.NotificationCompat
import com.androidmac.client.R
import com.androidmac.client.control.ControlClient
import com.androidmac.client.touch.TouchEvent
import com.androidmac.client.touch.TouchSender
import com.androidmac.client.video.TcpVideoReceiver
import com.androidmac.client.video.UdpReceiver
import com.androidmac.client.video.VideoPacket
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream
import java.io.IOException

/**
 * DisplayServiceはフォアグラウンドサービスで、すべての接続状態（制御・映像・タッチ）を管理し、
 * 通知バーに接続品質と切断ボタンを表示する。
 *
 * ライフサイクル：
 * 1. MainActivityがハンドシェイク完了後にこのサービスを起動
 * 2. サービスがUDP受信・タッチ接続を初期化
 * 3. DisplayActivityがサービスにバインドし、映像コールバックを登録
 * 4. ユーザーが切断を押すか接続が切れる → サービスがクリーンアップして停止
 */
class DisplayService : Service() {

    companion object {
        private const val TAG = "DisplayService"
        private const val CHANNEL_ID = "display_service"
        private const val NOTIFICATION_ID = 1
        private const val ACTION_DISCONNECT = "com.androidmac.client.DISCONNECT"
        private const val STATS_UPDATE_INTERVAL = 2000L // 2秒ごとに更新

        /**
         * MainActivityがハンドシェイク成功後にControlClientを一時的にここへ保存し、
         * サービス起動後すぐに取り出す。
         */
        @Volatile
        var pendingControlClient: ControlClient? = null
    }

    // Binder for DisplayActivity
    inner class LocalBinder : Binder() {
        fun getService(): DisplayService = this@DisplayService
    }

    private val binder = LocalBinder()

    /** 映像パケットのコールバック。DisplayActivityが登録する */
    interface VideoCallback {
        fun onNAL(data: ByteArray, frameType: Byte, timestamp: Long)
    }

    /** 接続切断のコールバック。DisplayActivityが登録する */
    interface DisconnectCallback {
        fun onDisconnected()
    }

    // 接続状態
    private var controlClient: ControlClient? = null
    private var udpReceiver: UdpReceiver? = null
    private var tcpVideoReceiver: TcpVideoReceiver? = null
    private var touchSender: TouchSender? = null
    private var receiveJob: Job? = null
    private var touchJob: Job? = null
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    // Buffered channel for touch events — single consumer, no per-event coroutine overhead
    private val touchChannel = Channel<TouchEvent>(capacity = 64)

    // コールバック
    @Volatile
    var videoCallback: VideoCallback? = null
    @Volatile
    var disconnectCallback: DisconnectCallback? = null

    // 直近のSPS/PPSをキャッシュし、デコーダー再起動時に注入する（例：ロック画面解除後など）
    @Volatile
    var cachedSPS: ByteArray? = null
        private set
    @Volatile
    var cachedPPS: ByteArray? = null
        private set

    // 接続パラメータ
    private var serverHost: String = ""
    private var touchPort: Int = 0
    private var videoPort: Int = 0
    private var videoWidth: Int = 0
    private var videoHeight: Int = 0
    private var connectionType: String = "wifi"

    // フラグメント再構成の状態
    private var currentSequence: Long = -1
    private var currentFrameType: Byte = 0
    private var currentTimestamp: Long = 0
    private val fragments = mutableMapOf<Int, ByteArray>()
    private var expectedFragTotal: Int = 0

    // 統計情報
    private var packetCount: Long = 0
    private var submitCount: Long = 0
    private var dropCount: Long = 0
    private var startTime: Long = 0
    private var lastSubmitCount: Long = 0
    private var lastStatsTime: Long = 0

    // 通知の更新
    private lateinit var notificationManager: NotificationManager
    private val statsHandler = Handler(Looper.getMainLooper())
    private val statsRunnable = object : Runnable {
        override fun run() {
            updateNotificationStats()
            statsHandler.postDelayed(this, STATS_UPDATE_INTERVAL)
        }
    }

    // 切断ブロードキャストレシーバー
    private val disconnectReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            Log.d(TAG, "Disconnect action received")
            disconnect()
        }
    }

    override fun onCreate() {
        super.onCreate()
        notificationManager = getSystemService(NotificationManager::class.java)
        createNotificationChannel()

        // 切断ブロードキャストを登録
        registerReceiver(
            disconnectReceiver,
            IntentFilter(ACTION_DISCONNECT),
            RECEIVER_NOT_EXPORTED
        )
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // 即座にフォアグラウンドモードへ移行
        startForeground(NOTIFICATION_ID, buildNotification(getString(R.string.notification_connecting)))

        // 接続パラメータを取得
        serverHost = intent?.getStringExtra("serverHost") ?: ""
        touchPort = intent?.getIntExtra("touchPort", 0) ?: 0
        videoPort = intent?.getIntExtra("videoPort", 0) ?: 0
        videoWidth = intent?.getIntExtra("width", 1920) ?: 1920
        videoHeight = intent?.getIntExtra("height", 1080) ?: 1080
        connectionType = intent?.getStringExtra("connectionType") ?: "wifi"
        Log.d(TAG, "Connection: type=$connectionType host=$serverHost touchPort=$touchPort videoPort=$videoPort")

        // ControlClientを取得
        controlClient = pendingControlClient
        pendingControlClient = null

        if (controlClient == null) {
            Log.e(TAG, "No ControlClient available, stopping service")
            stopSelf()
            return START_NOT_STICKY
        }

        startTime = System.currentTimeMillis()
        lastStatsTime = startTime

        // 接続パイプラインを起動
        startPipeline()

        return START_NOT_STICKY
    }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        super.onDestroy()
        statsHandler.removeCallbacks(statsRunnable)
        try {
            unregisterReceiver(disconnectReceiver)
        } catch (_: Exception) {}
        cleanupConnections()
        serviceScope.cancel()
        Log.d(TAG, "Service destroyed")
    }

    /**
     * 映像受信＋タッチ接続パイプラインを起動する。
     * connectionTypeに応じてUDP（WiFi）またはTCP（USB）伝送を選択する。
     */
    private fun startPipeline() {
        serviceScope.launch {
            try {
                if (connectionType == "usb") {
                    startPipelineUSB()
                } else {
                    startPipelineWiFi()
                }

                // タッチ接続を起動
                startTouchConnection()

                // 制御接続を監視
                monitorControlConnection()

                // 通知を「接続済み」に更新
                Handler(Looper.getMainLooper()).post {
                    val modeLabel = if (connectionType == "usb") "USB" else "WiFi"
                    updateNotification("${getString(R.string.notification_connected)} [$modeLabel]")
                    statsHandler.postDelayed(statsRunnable, STATS_UPDATE_INTERVAL)
                }

                Log.d(TAG, "Pipeline started successfully ($connectionType)")
            } catch (e: Exception) {
                Log.e(TAG, "Pipeline start failed: ${e.message}", e)
                Handler(Looper.getMainLooper()).post {
                    disconnect()
                }
            }
        }
    }

    /** WiFiモード：UDP socketをバインド → ClientReadyを送信（UDP portを含む） → UDP受信を開始 */
    private suspend fun startPipelineWiFi() {
        val receiver = UdpReceiver(0)
        val actualPort = receiver.bind()
        udpReceiver = receiver
        Log.d(TAG, "WiFi: UDP bound to port $actualPort")

        controlClient?.sendReady(actualPort)
        Log.d(TAG, "WiFi: Sent ClientReady with UDP port $actualPort")

        receiveJob = serviceScope.launch {
            receiver.receiveLoop { packet ->
                handlePacket(packet)
            }
        }
    }

    /**
     * USBモード：ClientReadyを送信（UDPPort=0） → TCP映像に接続 → TCP受信を開始
     */
    private suspend fun startPipelineUSB() {
        // USBモードではUDPPort = 0とし、serverにTCP映像を使うよう伝える
        controlClient?.sendReady(0)
        Log.d(TAG, "USB: Sent ClientReady (TCP mode)")

        val receiver = TcpVideoReceiver(serverHost, videoPort)
        receiver.connect()
        tcpVideoReceiver = receiver
        Log.d(TAG, "USB: TCP video connected to $serverHost:$videoPort")

        receiveJob = serviceScope.launch {
            receiver.receiveLoop { packet ->
                handlePacket(packet)
            }
        }
    }

    /** TCP制御接続が切断されたかを監視する */
    private fun monitorControlConnection() {
        serviceScope.launch {
            try {
                val socket = controlClient?.getSocket() ?: return@launch
                val buf = ByteArray(1)
                while (true) {
                    val n = socket.getInputStream().read(buf)
                    if (n == -1) {
                        Log.d(TAG, "Control connection closed by server")
                        Handler(Looper.getMainLooper()).post { disconnect() }
                        return@launch
                    }
                }
            } catch (e: IOException) {
                Log.d(TAG, "Control connection lost: ${e.message}")
                Handler(Looper.getMainLooper()).post { disconnect() }
            }
        }
    }

    /**
     * タッチ用TCP接続を起動し、Channelのコンシューマーcoroutineも起動する。
     * 単一のcoroutineが全タッチイベントを消費し、イベントごとにlaunchするスケジューリングオーバーヘッドを避ける。
     */
    private fun startTouchConnection() {
        if (touchPort <= 0 || serverHost.isEmpty()) {
            Log.w(TAG, "Touch not available: port=$touchPort host=$serverHost")
            return
        }

        serviceScope.launch {
            try {
                val sender = TouchSender()
                sender.connect(serverHost, touchPort)
                touchSender = sender
                Log.d(TAG, "Touch connected to $serverHost:$touchPort")

                // Single consumer coroutine — drains touchChannel on IO thread
                touchJob = serviceScope.launch(Dispatchers.IO) {
                    for (event in touchChannel) {
                        try {
                            sender.sendDirect(event)
                        } catch (e: Exception) {
                            Log.e(TAG, "Touch send error: ${e.message}")
                            break
                        }
                    }
                }
            } catch (e: Exception) {
                Log.e(TAG, "Touch connection failed: ${e.message}", e)
            }
        }
    }

    /**
     * タッチイベントを送信する（DisplayActivityから呼ばれる）。
     * launchの代わりにChannel.trySendを使い、イベントごとにcoroutineを生成するオーバーヘッドを避ける。
     */
    fun sendTouchEvent(event: TouchEvent) {
        touchChannel.trySend(event)
    }

    /**
     * 受信したUDPパケットを処理し、フラグメントを再構成する。
     * SPS/PPSはキャッシュされ、デコーダー再起動時に即座に注入される。
     */
    private fun handlePacket(packet: VideoPacket) {
        packetCount++

        if (packet.fragTotal <= 1) {
            // デコーダー再起動時の復元用にSPS/PPSをキャッシュ
            when (packet.frameType) {
                0x10.toByte() -> cachedSPS = packet.payload.copyOf()
                0x11.toByte() -> cachedPPS = packet.payload.copyOf()
            }
            // 単一フラグメントのNAL、そのまま提出
            submitCount++
            videoCallback?.onNAL(packet.payload, packet.frameType, packet.timestamp)
            return
        }

        // 複数フラグメントの再構成
        synchronized(this) {
            if (packet.sequence != currentSequence) {
                // 新しいframe —— 前回が未完成なら破棄として記録
                if (currentSequence >= 0 && fragments.size < expectedFragTotal) {
                    dropCount++
                }
                currentSequence = packet.sequence
                currentFrameType = packet.frameType
                currentTimestamp = packet.timestamp
                fragments.clear()
                expectedFragTotal = packet.fragTotal
            }

            fragments[packet.fragIndex] = packet.payload

            if (fragments.size == expectedFragTotal) {
                // 全フラグメントが揃った、再構成する
                val assembled = ByteArrayOutputStream()
                for (i in 0 until expectedFragTotal) {
                    val frag = fragments[i]
                    if (frag != null) {
                        assembled.write(frag)
                    } else {
                        dropCount++
                        fragments.clear()
                        return
                    }
                }
                val ft = currentFrameType
                val ts = currentTimestamp
                val assembledData = assembled.toByteArray()
                fragments.clear()
                submitCount++
                videoCallback?.onNAL(assembledData, ft, ts)
            }
        }
    }

    /** 現在の接続統計を取得する */
    fun getStats(): ConnectionStats {
        val now = System.currentTimeMillis()
        val elapsed = (now - startTime) / 1000L
        val total = submitCount + dropCount
        val lossRate = if (total > 0) dropCount.toFloat() / total.toFloat() else 0f

        // リアルタイムFPSを計算
        val dt = (now - lastStatsTime) / 1000f
        val fps = if (dt > 0) (submitCount - lastSubmitCount).toFloat() / dt else 0f

        return ConnectionStats(
            fps = fps,
            packetLossRate = lossRate,
            totalPackets = packetCount,
            totalFrames = submitCount,
            droppedFrames = dropCount,
            uptimeSeconds = elapsed
        )
    }

    /** 接続を切断してサービスを停止する */
    fun disconnect() {
        Log.d(TAG, "Disconnecting...")
        disconnectCallback?.onDisconnected()
        cleanupConnections()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun cleanupConnections() {
        receiveJob?.cancel()
        receiveJob = null
        touchJob?.cancel()
        touchJob = null
        touchChannel.close()
        udpReceiver?.close()
        udpReceiver = null
        tcpVideoReceiver?.close()
        tcpVideoReceiver = null
        touchSender?.disconnect()
        touchSender = null
        controlClient?.disconnect()
        controlClient = null
        videoCallback = null
        disconnectCallback = null
    }

    // ——— 通知関連 ———

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.notification_channel_name),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = getString(R.string.notification_channel_desc)
            setShowBadge(false)
        }
        notificationManager.createNotificationChannel(channel)
    }

    private fun buildNotification(contentText: String): Notification {
        val disconnectIntent = Intent(ACTION_DISCONNECT).apply {
            setPackage(packageName)
        }
        val disconnectPending = PendingIntent.getBroadcast(
            this, 0, disconnectIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_display)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(contentText)
            .setOngoing(true)
            .setSilent(true)
            .setOnlyAlertOnce(true)
            .addAction(
                R.drawable.ic_disconnect,
                getString(R.string.notification_disconnect),
                disconnectPending
            )
            .build()
    }

    private fun updateNotification(contentText: String) {
        notificationManager.notify(NOTIFICATION_ID, buildNotification(contentText))
    }

    private fun updateNotificationStats() {
        val stats = getStats()
        lastSubmitCount = submitCount
        lastStatsTime = System.currentTimeMillis()

        val text = String.format(
            getString(R.string.notification_stats_format),
            stats.fps,
            stats.packetLossRate * 100f
        )
        updateNotification(text)
    }
}
