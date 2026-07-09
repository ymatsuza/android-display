package com.androidmac.client.service

/**
 * 接続品質の統計データ。DisplayServiceが計算し、通知バーの更新に使う。
 */
data class ConnectionStats(
    val fps: Float = 0f,
    val packetLossRate: Float = 0f,
    val totalPackets: Long = 0,
    val totalFrames: Long = 0,
    val droppedFrames: Long = 0,
    val uptimeSeconds: Long = 0
)
