package stream

// Streamer is the common interface for video transport (UDP or TCP).
// Both UDPStreamer and TCPVideoServer implement this interface,
// allowing the pipeline to be transport-agnostic.
type Streamer interface {
	// SendFrame sends a complete NAL unit with the given frame type.
	// The implementation handles fragmentation (UDP) or framing (TCP).
	SendFrame(nalUnit []byte, frameType byte) error

	// Close releases the underlying connection.
	Close() error
}
