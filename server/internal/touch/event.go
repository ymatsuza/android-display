package touch

import (
	"encoding/binary"
	"math"
)

type TouchType byte

const (
	TouchTypeFinger TouchType = 0
	TouchTypePen    TouchType = 1
)

type TouchAction byte

const (
	TouchActionDown TouchAction = 0
	TouchActionMove TouchAction = 1
	TouchActionUp   TouchAction = 2
)

// EventSize is the fixed byte size of a serialized touch event.
//
// Layout (34 bytes, big-endian):
//   Byte  0:     Type      (uint8)
//   Byte  1:     Action    (uint8)
//   Bytes 2-5:   X         (float32)
//   Bytes 6-9:   Y         (float32)
//   Bytes 10-13: Pressure  (float32)
//   Bytes 14-17: TiltX     (float32)
//   Bytes 18-21: TiltY     (float32)
//   Bytes 22-25: PointerID (int32)
//   Bytes 26-33: Timestamp (int64, milliseconds)
const EventSize = 34

type Event struct {
	Type      TouchType
	Action    TouchAction
	X         float32
	Y         float32
	Pressure  float32
	TiltX     float32
	TiltY     float32
	PointerID int32
	Timestamp int64
}

func (e *Event) Marshal() []byte {
	buf := make([]byte, EventSize)
	buf[0] = byte(e.Type)
	buf[1] = byte(e.Action)
	binary.BigEndian.PutUint32(buf[2:6], math.Float32bits(e.X))
	binary.BigEndian.PutUint32(buf[6:10], math.Float32bits(e.Y))
	binary.BigEndian.PutUint32(buf[10:14], math.Float32bits(e.Pressure))
	binary.BigEndian.PutUint32(buf[14:18], math.Float32bits(e.TiltX))
	binary.BigEndian.PutUint32(buf[18:22], math.Float32bits(e.TiltY))
	binary.BigEndian.PutUint32(buf[22:26], uint32(e.PointerID))
	binary.BigEndian.PutUint64(buf[26:34], uint64(e.Timestamp))
	return buf
}

func Unmarshal(buf []byte) Event {
	return Event{
		Type:      TouchType(buf[0]),
		Action:    TouchAction(buf[1]),
		X:         math.Float32frombits(binary.BigEndian.Uint32(buf[2:6])),
		Y:         math.Float32frombits(binary.BigEndian.Uint32(buf[6:10])),
		Pressure:  math.Float32frombits(binary.BigEndian.Uint32(buf[10:14])),
		TiltX:     math.Float32frombits(binary.BigEndian.Uint32(buf[14:18])),
		TiltY:     math.Float32frombits(binary.BigEndian.Uint32(buf[18:22])),
		PointerID: int32(binary.BigEndian.Uint32(buf[22:26])),
		Timestamp: int64(binary.BigEndian.Uint64(buf[26:34])),
	}
}
