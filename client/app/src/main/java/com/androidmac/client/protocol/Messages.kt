package com.androidmac.client.protocol

import org.json.JSONArray
import org.json.JSONObject

data class ScreenInfo(val width: Int, val height: Int, val dpi: Int)

data class ClientHello(
    val device: String,
    val screen: ScreenInfo,
    val capabilities: List<String>,
    val codecs: List<String>
) {
    fun toJson(): String {
        val obj = JSONObject()
        obj.put("device", device)
        obj.put("screen", JSONObject().apply {
            put("width", screen.width)
            put("height", screen.height)
            put("dpi", screen.dpi)
        })
        obj.put("capabilities", JSONArray(capabilities))
        obj.put("codecs", JSONArray(codecs))
        return obj.toString()
    }
}

data class DisplayInfo(val width: Int, val height: Int)

data class ServerHello(
    val virtualDisplay: DisplayInfo,
    val codec: String,
    val bitrate: Int,
    val fps: Int,
    val streamPort: Int
) {
    companion object {
        fun fromJson(json: String): ServerHello {
            val obj = JSONObject(json)
            val disp = obj.getJSONObject("virtualDisplay")
            return ServerHello(
                virtualDisplay = DisplayInfo(disp.getInt("width"), disp.getInt("height")),
                codec = obj.getString("codec"),
                bitrate = obj.getInt("bitrate"),
                fps = obj.getInt("fps"),
                streamPort = obj.getInt("streamPort")
            )
        }
    }
}
