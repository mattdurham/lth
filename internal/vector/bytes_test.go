// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"testing"
)

func TestToBytesFromBytes(t *testing.T) {
	tests := []struct {
		name string
		v    []float32
	}{
		{
			name: "empty vector",
			v:    []float32{},
		},
		{
			name: "single element",
			v:    []float32{3.14},
		},
		{
			name: "three elements",
			v:    []float32{1.0, -2.5, 0.0},
		},
		{
			name: "768 element vector",
			v:    make768Vector(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := ToBytes(tc.v)
			got := FromBytes(b)

			if len(got) != len(tc.v) {
				t.Fatalf("len(FromBytes(ToBytes(v))) = %d, want %d", len(got), len(tc.v))
			}

			for i := range tc.v {
				if got[i] != tc.v[i] {
					t.Errorf("FromBytes(ToBytes(v))[%d] = %v, want %v", i, got[i], tc.v[i])
				}
			}

			// Verify byte length is 4 * len(v)
			expectedBytes := len(tc.v) * 4
			if len(b) != expectedBytes {
				t.Errorf("ToBytes(v) length = %d, want %d", len(b), expectedBytes)
			}
		})
	}
}

func make768Vector() []float32 {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i) * 0.001
	}
	return v
}
