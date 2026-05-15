// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
		eps  float32
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0, 0},
			want: 1.0,
			eps:  1e-6,
		},
		{
			name: "zero vectors",
			a:    []float32{0, 0, 0},
			b:    []float32{0, 0, 0},
			want: 0.0,
			eps:  1e-6,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 0.0,
			eps:  1e-6,
		},
		{
			name: "opposite direction",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: -1.0,
			eps:  1e-6,
		},
		{
			name: "45 degrees",
			a:    []float32{1, 0},
			b:    []float32{1, 1},
			want: float32(1.0 / math.Sqrt2),
			eps:  1e-5,
		},
		{
			name: "different length vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0},
			want: 0.0,
			eps:  1e-6,
		},
		{
			name: "unit vectors with value",
			a:    []float32{0.6, 0.8},
			b:    []float32{0.6, 0.8},
			want: 1.0,
			eps:  1e-5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Cosine(tc.a, tc.b)
			diff := got - tc.want
			if diff < 0 {
				diff = -diff
			}
			if diff > tc.eps {
				t.Errorf("Cosine(%v, %v) = %f, want %f (eps %f)", tc.a, tc.b, got, tc.want, tc.eps)
			}
		})
	}
}
