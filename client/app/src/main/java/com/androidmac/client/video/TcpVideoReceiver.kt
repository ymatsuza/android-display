package com.androidmac.client.video

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.io.BufferedInputStream
import java.io.DataInputStream
import java.net.Socket

/**
 * TCP映像レシーバー — USBモードでUdpReceiverの代わりに使う。
 *
 * serverのTCP video portに接続し、length-prefixedパケットを読み取る：
 *   [4-byte length (big-endian)] [17-byte header] [payload]
 *
 * TCPは順序と完全性を保証するため、フラグメント再構成は不要 —
 * 各パケットは完全なNAL unit（fragIndex=0, fragTotal=1）である。
 */
class TcpVideoReceiver(private val host: String, private val port: Int) {

    companion object {
        private const val TAG = "TcpVideoReceiver"
    }

    private var socket: Socket? = null
    private var input: DataInputStream? = null

    /**
     * server video portへTCP接続を確立する。
     */
    suspend fun connect() = withContext(Dispatchers.IO) {
        val sock = Socket(host, port)
        sock.tcpNoDelay = true
        sock.receiveBufferSize = 4 * 1024 * 1024 // match server write buffer
        socket = sock
        input = DataInputStream(BufferedInputStream(sock.getInputStream(), 256 * 1024))
        Log.d(TAG, "Connected to $host:$port")
    }

    /**
     * coroutineがキャンセルされるか接続が切れるまで、TCP映像パケットを継続受信する。
     */
    suspend fun receiveLoop(onPacket: (VideoPacket) -> Unit) = withContext(Dispatchers.IO) {
        val inp = input ?: throw IllegalStateException("Not connected. Call connect() first.")

        // Reusable read buffer — grown on demand, avoids per-packet allocation.
        // Most NAL units are < 64KB; keyframes may be larger.
        var readBuf = ByteArray(64 * 1024)

        while (isActive) {
            try {
                // 4-byte length prefixを読み取る
                val totalLen = inp.readInt()
                if (totalLen <= 0 || totalLen > 2 * 1024 * 1024) {
                    Log.w(TAG, "Invalid packet length: $totalLen, skipping")
                    continue
                }

                // Grow buffer if needed (keeps the larger size for subsequent frames)
                if (readBuf.size < totalLen) {
                    readBuf = ByteArray(totalLen)
                }

                // 完全なパケット（header + payload）を再利用可能なbufferに読み込む
                inp.readFully(readBuf, 0, totalLen)

                // パケットを解析
                val packet = PacketParser.parse(readBuf, 0, totalLen)
                onPacket(packet)
            } catch (e: Exception) {
                if (isActive) {
                    Log.e(TAG, "Receive error: ${e.message}")
                }
                break
            }
        }
    }

    fun close() {
        try {
            socket?.close()
        } catch (_: Exception) {}
        socket = null
        input = null
    }
}
