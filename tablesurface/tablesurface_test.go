package tablesurface

import (
	"math"
	"testing"
)

func TestFitPlaneExact(t *testing.T) {
	// Three points on the plane z = 0.01*x - 0.02*y + 300.
	points := [][]float64{
		{150, -127, 0.01*150 - 0.02*-127 + 300},
		{450, -127, 0.01*450 - 0.02*-127 + 300},
		{150, 123, 0.01*150 - 0.02*123 + 300},
	}
	a, b, c, err := fitPlane(points)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const tol = 1e-9
	if math.Abs(a-0.01) > tol || math.Abs(b+0.02) > tol || math.Abs(c-300) > tol {
		t.Errorf("expected plane (0.01, -0.02, 300), got (%v, %v, %v)", a, b, c)
	}
}

func TestFitPlaneLeastSquares(t *testing.T) {
	// Four points on z = 302 with +/-0.5mm probing noise on two of them; the
	// fit should land near flat z = 302.
	points := [][]float64{
		{100, 100, 302.5},
		{400, 100, 301.5},
		{100, 300, 302},
		{400, 300, 302},
	}
	a, b, c, err := fitPlane(points)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range []([2]float64){{250, 200}, {100, 100}, {400, 300}} {
		z := a*p[0] + b*p[1] + c
		if math.Abs(z-302) > 0.5 {
			t.Errorf("fitted z at (%v, %v) = %v, want within 0.5 of 302", p[0], p[1], z)
		}
	}
}

func TestFitPlaneCollinear(t *testing.T) {
	points := [][]float64{
		{100, 100, 300},
		{200, 200, 301},
		{300, 300, 302},
	}
	if _, _, _, err := fitPlane(points); err == nil {
		t.Fatal("expected an error for collinear points")
	}
}
