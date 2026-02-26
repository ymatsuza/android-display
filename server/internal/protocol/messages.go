package protocol

// ClientHello is sent by Android to Mac during handshake
type ClientHello struct {
	Device       string     `json:"device"`
	Screen       ScreenInfo `json:"screen"`
	Capabilities []string   `json:"capabilities"`
	Codecs       []string   `json:"codecs"`
}

type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	DPI    int `json:"dpi"`
}

// ServerHello is sent by Mac to Android in response
type ServerHello struct {
	VirtualDisplay DisplayInfo `json:"virtualDisplay"`
	Codec          string      `json:"codec"`
	Bitrate        int         `json:"bitrate"`
	FPS            int         `json:"fps"`
	StreamPort     int         `json:"streamPort"`
}

type DisplayInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ClientReady is sent by Android to Mac after binding the UDP socket.
// It reports the actual listening port so the server can stream to it.
type ClientReady struct {
	UDPPort int `json:"udpPort"`
}

// Heartbeat is sent periodically on the control channel
type Heartbeat struct {
	Timestamp int64 `json:"timestamp"`
}
