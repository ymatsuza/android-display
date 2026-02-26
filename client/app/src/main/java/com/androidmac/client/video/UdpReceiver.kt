package com.androidmac.client.video

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.nio.ByteBuffer
import java.nio.ByteOrder

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
        private const val HEADER_SIZE = 15
        private const val MAX_PACKET = 1500
    }

    suspend fun receiveLoop(onPacket: (VideoPacket) -> Unit) = withContext(Dispatchers.IO) {
        val sock = DatagramSocket(port)
        sock.receiveBufferSize = 2 * 1024 * 1024
        socket = sock

        val buf = ByteArray(MAX_PACKET)
        val packet = DatagramPacket(buf, buf.size)

        while (isActive) {
            try {
                sock.receive(packet)
                if (packet.length < HEADER_SIZE) continue

                val bb = ByteBuffer.wrap(buf, 0, packet.length).order(ByteOrder.BIG_ENDIAN)
                val seq = bb.int.toLong() and 0xFFFFFFFFL
                val ts = bb.long
                val frameType = bb.get()
                val fragIndex = bb.get().toInt() and 0xFF
                val fragTotal = bb.get().toInt() and 0xFF

                val payloadSize = packet.length - HEADER_SIZE
                val payload = ByteArray(payloadSize)
                System.arraycopy(buf, HEADER_SIZE, payload, 0, payloadSize)

                onPacket(VideoPacket(seq, ts, frameType, fragIndex, fragTotal, payload))
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
