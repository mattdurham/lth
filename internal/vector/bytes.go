// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"encoding/binary"
	"math"
)

// ToBytes encodes a float32 slice as IEEE 754 little-endian bytes.
// Each float32 is encoded as 4 bytes. Returns nil for a nil or empty input.
func ToBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// FromBytes decodes IEEE 754 little-endian bytes to a float32 slice.
// Returns nil if b is empty or its length is not a multiple of 4 bytes.
func FromBytes(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
