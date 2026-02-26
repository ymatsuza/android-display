package com.androidmac.client.service

/**
 * 連線品質統計數據，由 DisplayService 計算並用於更新通知列。
 */
data class ConnectionStats(
    val fps: Float = 0f,
    val packetLossRate: Float = 0f,
    val totalPackets: Long = 0,
    val totalFrames: Long = 0,
    val droppedFrames: Long = 0,
    val uptimeSeconds: Long = 0
)
