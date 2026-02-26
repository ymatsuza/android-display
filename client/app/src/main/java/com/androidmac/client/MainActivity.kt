package com.androidmac.client

import android.content.Intent
import android.os.Bundle
import android.util.Log
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.control.ControlClient
import com.androidmac.client.discovery.NsdDiscovery
import com.google.android.material.textfield.TextInputEditText
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.launch

class MainActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "MainActivity"
        private const val DEFAULT_PORT = 9000
    }

    private lateinit var statusText: TextView
    private lateinit var manualIpInput: TextInputEditText
    private lateinit var connectButton: Button

    private var discovery: NsdDiscovery? = null
    private var discoveryJob: Job? = null
    private val controlClient = ControlClient()

    private var discoveredHost: String? = null
    private var discoveredPort: Int = DEFAULT_PORT

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        statusText = findViewById(R.id.statusText)
        manualIpInput = findViewById(R.id.manualIpInput)
        connectButton = findViewById(R.id.connectButton)

        connectButton.setOnClickListener { onConnectClicked() }

        startDiscovery()
    }

    private fun startDiscovery() {
        discovery = NsdDiscovery(this)
        discoveryJob = lifecycleScope.launch {
            discovery!!.discover()
                .catch { e -> Log.e(TAG, "Discovery error: ${e.message}") }
                .collect { server ->
                    discoveredHost = server.host
                    discoveredPort = server.port
                    statusText.text = "Found: ${server.name} (${server.host}:${server.port})"
                    manualIpInput.setText(server.host)
                    Log.d(TAG, "Discovered server: $server")
                }
        }
    }

    private fun onConnectClicked() {
        val ip = manualIpInput.text?.toString()?.trim()
        if (ip.isNullOrEmpty()) {
            Toast.makeText(this, "Enter an IP address or wait for discovery", Toast.LENGTH_SHORT).show()
            return
        }

        val host = ip
        val port = discoveredPort

        connectButton.isEnabled = false
        statusText.text = "Connecting to $host:$port..."

        lifecycleScope.launch {
            try {
                val metrics = resources.displayMetrics
                val serverHello = controlClient.connect(host, port, metrics)

                Log.d(TAG, "Handshake complete: $serverHello")

                // Store the ControlClient reference for DisplayActivity
                DisplayActivity.pendingControlClient = controlClient

                val intent = Intent(this@MainActivity, DisplayActivity::class.java).apply {
                    putExtra("width", serverHello.virtualDisplay.width)
                    putExtra("height", serverHello.virtualDisplay.height)
                    putExtra("touchPort", serverHello.touchPort)
                    putExtra("serverHost", host)
                }
                startActivity(intent)
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
        controlClient.disconnect()
    }
}
