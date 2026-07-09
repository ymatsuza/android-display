package com.androidmac.client.video

import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * 共通の17-byte packet header解析ユーティリティ。
 * UDPとTCPの両レシーバーがこのパーサーを使用する。
 *
 * Header format (17 bytes, big-endian):
 *   [4] Sequence  (uint32)
 *   [8] Timestamp (uint64)
 *   [1] FrameType (byte)
 *   [2] FragIndex (uint16)
 *   [2] FragTotal (uint16)
 */
object PacketParser {
    const val HEADER_SIZE = 17

    fun parse(data: ByteArray, offset: Int = 0, length: Int = data.size): VideoPacket {
        require(length >= HEADER_SIZE) { "Packet too small: $length < $HEADER_SIZE" }

        val bb = ByteBuffer.wrap(data, offset, length).order(ByteOrder.BIG_ENDIAN)
        val seq = bb.int.toLong() and 0xFFFFFFFFL
        val ts = bb.long
        val frameType = bb.get()
        val fragIndex = bb.short.toInt() and 0xFFFF
        val fragTotal = bb.short.toInt() and 0xFFFF

        val payloadSize = length - HEADER_SIZE
        val payload = ByteArray(payloadSize)
        System.arraycopy(data, offset + HEADER_SIZE, payload, 0, payloadSize)

        return VideoPacket(seq, ts, frameType, fragIndex, fragTotal, payload)
    }
}
