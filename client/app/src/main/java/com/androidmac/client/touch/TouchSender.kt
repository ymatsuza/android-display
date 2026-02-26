package com.androidmac.client.touch

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.BufferedOutputStream
import java.io.OutputStream
import java.net.Socket

class TouchSender {
    private var socket: Socket? = null
    private var output: OutputStream? = null

    suspend fun connect(host: String, port: Int) = withContext(Dispatchers.IO) {
        socket = Socket(host, port).also {
            it.tcpNoDelay = true
            output = BufferedOutputStream(it.getOutputStream())
        }
    }

    suspend fun send(event: TouchEvent) = withContext(Dispatchers.IO) {
        output?.let {
            it.write(event.toBytes())
            it.flush()
        }
    }

    fun disconnect() {
        try { socket?.close() } catch (_: Exception) {}
        socket = null
        output = null
    }
}
