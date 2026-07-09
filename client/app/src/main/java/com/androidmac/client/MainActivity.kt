package com.androidmac.client

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.CheckBox
import android.widget.RadioGroup
import android.widget.Spinner
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.control.ControlClient
import com.androidmac.client.discovery.NsdDiscovery
import com.androidmac.client.service.DisplayService
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.launch

class MainActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "MainActivity"
        private const val DEFAULT_PORT = 9000
        private const val NOTIFICATION_PERMISSION_CODE = 1001
    }

    private lateinit var statusText: TextView
    private lateinit var manualIpInput: TextInputEditText
    private lateinit var ipInputLayout: TextInputLayout
    private lateinit var connectButton: Button
    private lateinit var connectionMode: RadioGroup
    private lateinit var orientationMode: RadioGroup
    private lateinit var orientationReverse: CheckBox
    private lateinit var resolutionSpinner: Spinner
    private lateinit var bitrateSpinner: Spinner

    // Resolution scale factors corresponding to the string-array entries
    private val scaleFactors = floatArrayOf(1.0f, 0.75f, 0.5f)

    // Bitrate values in bps corresponding to the string-array entries
    private val bitrateValues = intArrayOf(8_000_000, 4_000_000, 2_000_000, 1_000_000)

    private var discovery: NsdDiscovery? = null
    private var discoveryJob: Job? = null

    private var discoveredHost: String? = null
    private var discoveredPort: Int = DEFAULT_PORT
    private var isUsbMode: Boolean = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        statusText = findViewById(R.id.statusText)
        manualIpInput = findViewById(R.id.manualIpInput)
        ipInputLayout = findViewById(R.id.ipInputLayout)
        connectButton = findViewById(R.id.connectButton)
        connectionMode = findViewById(R.id.connectionMode)
        orientationMode = findViewById(R.id.orientationMode)
        orientationReverse = findViewById(R.id.orientationReverse)
        resolutionSpinner = findViewById(R.id.resolutionSpinner)
        bitrateSpinner = findViewById(R.id.bitrateSpinner)

        // 解像度選択ドロップダウン
        ArrayAdapter.createFromResource(
            this, R.array.resolution_options, android.R.layout.simple_spinner_item
        ).also { adapter ->
            adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
            resolutionSpinner.adapter = adapter
        }

        // 画質選択ドロップダウン
        ArrayAdapter.createFromResource(
            this, R.array.bitrate_options, android.R.layout.simple_spinner_item
        ).also { adapter ->
            adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
            bitrateSpinner.adapter = adapter
        }

        connectButton.setOnClickListener { onConnectClicked() }

        // WiFi / USB モード切り替え
        connectionMode.setOnCheckedChangeListener { _, checkedId ->
            isUsbMode = (checkedId == R.id.modeUsb)
            if (isUsbMode) {
                // USBモード：localhost固定、IP入力と検索状態を隠す
                manualIpInput.setText("127.0.0.1")
                ipInputLayout.visibility = View.GONE
                statusText.text = "USBモード — デバイスがUSB接続されていることを確認してください"
                discoveryJob?.cancel()
            } else {
                // WiFiモード：検索を再開
                ipInputLayout.visibility = View.VISIBLE
                manualIpInput.setText(discoveredHost ?: "")
                statusText.text = "Scanning..."
                startDiscovery()
            }
        }

        // 通知権限をリクエスト (Android 13+)
        requestNotificationPermission()

        startDiscovery()
    }

    private fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                ActivityCompat.requestPermissions(
                    this,
                    arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                    NOTIFICATION_PERMISSION_CODE
                )
            }
        }
    }

    private fun startDiscovery() {
        discoveryJob?.cancel()
        discovery = NsdDiscovery(this)
        discoveryJob = lifecycleScope.launch {
            discovery!!.discover()
                .catch { e -> Log.e(TAG, "Discovery error: ${e.message}") }
                .collect { server ->
                    discoveredHost = server.host
                    discoveredPort = server.port
                    if (!isUsbMode) {
                        statusText.text = "Found: ${server.name} (${server.host}:${server.port})"
                        manualIpInput.setText(server.host)
                    }
                    Log.d(TAG, "Discovered server: $server")
                }
        }
    }

    private fun onConnectClicked() {
        val connectionType = if (isUsbMode) "usb" else "wifi"
        val host = if (isUsbMode) "127.0.0.1" else {
            val ip = manualIpInput.text?.toString()?.trim()
            if (ip.isNullOrEmpty()) {
                Toast.makeText(this, "Enter an IP address or wait for discovery", Toast.LENGTH_SHORT).show()
                return
            }
            ip
        }
        val port = DEFAULT_PORT

        connectButton.isEnabled = false
        statusText.text = "Connecting to $host:$port ($connectionType)..."

        lifecycleScope.launch {
            try {
                // MainActivityでハンドシェイクを実行し、エラーを即座にフィードバック
                val controlClient = ControlClient()
                val metrics = resources.displayMetrics
                val isPortrait = orientationMode.checkedRadioButtonId == R.id.orientationPortrait

                // 解像度スケーリングを適用（比率維持、H.264は偶数の幅・高さが必要）
                val scale = scaleFactors[resolutionSpinner.selectedItemPosition]
                val rawLongSide = maxOf(metrics.widthPixels, metrics.heightPixels) * scale
                val rawShortSide = minOf(metrics.widthPixels, metrics.heightPixels) * scale
                val longSide = rawLongSide.toInt() and -2 // round down to even
                val shortSide = rawShortSide.toInt() and -2
                val scaledWidth = if (isPortrait) shortSide else longSide
                val scaledHeight = if (isPortrait) longSide else shortSide
                val bitrate = bitrateValues[bitrateSpinner.selectedItemPosition]
                val serverHello = controlClient.connect(
                    host, port, scaledWidth, scaledHeight, metrics.densityDpi, connectionType, bitrate
                )

                Log.d(TAG, "Handshake complete: $serverHello")

                // ControlClientをDisplayServiceに一時保存
                DisplayService.pendingControlClient = controlClient

                // フォアグラウンドサービスを起動
                val serviceIntent = Intent(this@MainActivity, DisplayService::class.java).apply {
                    putExtra("serverHost", host)
                    putExtra("touchPort", serverHello.touchPort)
                    putExtra("videoPort", serverHello.videoPort)
                    putExtra("width", serverHello.virtualDisplay.width)
                    putExtra("height", serverHello.virtualDisplay.height)
                    putExtra("connectionType", connectionType)
                }
                startForegroundService(serviceIntent)

                // DisplayActivityを起動
                val displayIntent = Intent(this@MainActivity, DisplayActivity::class.java).apply {
                    putExtra("width", serverHello.virtualDisplay.width)
                    putExtra("height", serverHello.virtualDisplay.height)
                    putExtra("orientation", if (isPortrait) "portrait" else "landscape")
                    putExtra("reverse", orientationReverse.isChecked)
                }
                startActivity(displayIntent)
            } catch (e: Exception) {
                Log.e(TAG, "Connection failed: ${e.message}", e)
                Toast.makeText(
                    this@MainActivity,
                    "Connection failed: ${e.message}",
                    Toast.LENGTH_LONG
                ).show()
                statusText.text = "Connection failed. Tap Connect to retry."
            } finally {
                connectButton.isEnabled = true
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        discoveryJob?.cancel()
    }
}
