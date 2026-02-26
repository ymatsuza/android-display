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

const EventSize = 26

type Event struct {
	Type      TouchType
	Action    TouchAction
	X         float32
	Y         float32
	Pressure  float32
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
	binary.BigEndian.PutUint32(buf[14:18], uint32(e.PointerID))
	binary.BigEndian.PutUint64(buf[18:26], uint64(e.Timestamp))
	return buf
}

func Unmarshal(buf []byte) Event {
	return Event{
		Type:      TouchType(buf[0]),
		Action:    TouchAction(buf[1]),
		X:         math.Float32frombits(binary.BigEndian.Uint32(buf[2:6])),
		Y:         math.Float32frombits(binary.BigEndian.Uint32(buf[6:10])),
		Pressure:  math.Float32frombits(binary.BigEndian.Uint32(buf[10:14])),
		PointerID: int32(binary.BigEndian.Uint32(buf[14:18])),
		Timestamp: int64(binary.BigEndian.Uint64(buf[18:26])),
	}
}
