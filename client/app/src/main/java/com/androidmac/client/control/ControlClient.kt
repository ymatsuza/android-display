package com.androidmac.client.control

import android.util.DisplayMetrics
import com.androidmac.client.protocol.ClientHello
import com.androidmac.client.protocol.ScreenInfo
import com.androidmac.client.protocol.ServerHello
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.BufferedReader
import java.io.InputStreamReader
import java.io.PrintWriter
import java.net.Socket

class ControlClient {
    private var socket: Socket? = null

    suspend fun connect(host: String, port: Int, metrics: DisplayMetrics): ServerHello =
        withContext(Dispatchers.IO) {
            val sock = Socket(host, port)
            sock.tcpNoDelay = true
            socket = sock

            val writer = PrintWriter(sock.getOutputStream(), true)
            val reader = BufferedReader(InputStreamReader(sock.getInputStream()))

            val hello = ClientHello(
                device = android.os.Build.MODEL,
                screen = ScreenInfo(
                    metrics.widthPixels,
                    metrics.heightPixels,
                    metrics.densityDpi
                ),
                capabilities = listOf("touch", "pen", "pressure"),
                codecs = listOf("h264")
            )
            writer.println(hello.toJson())

            val response = reader.readLine() ?: throw Exception("Server closed connection")
            ServerHello.fromJson(response)
        }

    fun disconnect() {
        try {
            socket?.close()
        } catch (_: Exception) {
        }
        socket = null
    }
}
