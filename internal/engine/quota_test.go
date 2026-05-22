package engine

import "testing"

func TestCalculateLimit(t *testing.T) {
	if got := CalculateLimit(180, 0.5, 20); got != 90 {
		t.Fatalf("limit mismatch: got %v want 90", got)
	}
	if got := CalculateLimit(10, 0.5, 20); got != 20 {
		t.Fatalf("min limit mismatch: got %v want 20", got)
	}
}
