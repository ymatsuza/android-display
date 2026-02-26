package com.androidmac.client.video

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.net.DatagramPacket
import java.net.DatagramSocket

data class VideoPacket(
    val sequence: Long,
    val timestamp: Long,
    val frameType: Byte,
    val fragIndex: Int,
    val fragTotal: Int,
    val payload: ByteArray
) {
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (other !is VideoPacket) return false
        return sequence == other.sequence &&
            timestamp == other.timestamp &&
            frameType == other.frameType &&
            fragIndex == other.fragIndex &&
            fragTotal == other.fragTotal &&
            payload.contentEquals(other.payload)
    }

    override fun hashCode(): Int {
        var result = sequence.hashCode()
        result = 31 * result + timestamp.hashCode()
        result = 31 * result + frameType.hashCode()
        result = 31 * result + fragIndex
        result = 31 * result + fragTotal
        result = 31 * result + payload.contentHashCode()
        return result
    }
}

class UdpReceiver(private val port: Int) {
    private var socket: DatagramSocket? = null

    companion object {
        private const val TAG = "UdpReceiver"
        private const val MAX_PACKET = 1500
    }

    /**
     * Bind the UDP socket immediately and return the actual local port.
     * Pass port=0 to let the OS auto-assign an available port.
     */
    fun bind(): Int {
        val sock = DatagramSocket(port)
        sock.receiveBufferSize = 2 * 1024 * 1024
        socket = sock
        val localPort = sock.localPort
        Log.d(TAG, "UDP socket bound to port $localPort")
        return localPort
    }

    /**
     * Returns the actual port the socket is bound to, or -1 if not bound.
     */
    fun getLocalPort(): Int = socket?.localPort ?: -1

    suspend fun receiveLoop(onPacket: (VideoPacket) -> Unit) = withContext(Dispatchers.IO) {
        val sock = socket ?: throw IllegalStateException("Socket not bound. Call bind() first.")

        val buf = ByteArray(MAX_PACKET)
        val packet = DatagramPacket(buf, buf.size)

        while (isActive) {
            try {
                sock.receive(packet)
                if (packet.length < PacketParser.HEADER_SIZE) continue

                val videoPacket = PacketParser.parse(buf, 0, packet.length)
                onPacket(videoPacket)
            } catch (e: Exception) {
                if (isActive) Log.e(TAG, "Receive error: ${e.message}")
            }
        }
    }

    fun close() {
        socket?.close()
        socket = null
    }
}
