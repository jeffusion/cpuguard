package engine

import (
	"testing"
	"time"
)

func TestWindowRatios(t *testing.T) {
	var w Window
	now := time.Now()
	for i := 0; i < 10; i++ {
		value := 95.0
		if i >= 9 {
			value = 50
		}
		w.Add(Sample{At: now.Add(time.Duration(i) * time.Second), CPUPct: value}, 10*time.Second)
	}
	ratio := w.RatioAbove(90, 10*time.Second, now.Add(9*time.Second))
	if ratio != 0.9 {
		t.Fatalf("unexpected ratio: got %.2f want 0.9", ratio)
	}
}
