package com.androidmac.client.touch

import java.nio.ByteBuffer
import java.nio.ByteOrder

data class TouchEvent(
    val type: Byte,
    val action: Byte,
    val x: Float,
    val y: Float,
    val pressure: Float,
    val tiltX: Float,
    val tiltY: Float,
    val pointerId: Int,
    val timestamp: Long
) {
    companion object {
        const val SIZE = 34
        const val TYPE_FINGER: Byte = 0
        const val TYPE_PEN: Byte = 1
        const val ACTION_DOWN: Byte = 0
        const val ACTION_MOVE: Byte = 1
        const val ACTION_UP: Byte = 2
    }

    fun toBytes(): ByteArray {
        val buf = ByteBuffer.allocate(SIZE).order(ByteOrder.BIG_ENDIAN)
        buf.put(type)
        buf.put(action)
        buf.putFloat(x)
        buf.putFloat(y)
        buf.putFloat(pressure)
        buf.putFloat(tiltX)
        buf.putFloat(tiltY)
        buf.putInt(pointerId)
        buf.putLong(timestamp)
        return buf.array()
    }
}
