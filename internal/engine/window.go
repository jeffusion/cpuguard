package engine

import "time"

type Sample struct {
	At     time.Time
	CPUPct float64
}

type Window struct {
	samples []Sample
}

func (w *Window) Add(s Sample, maxAge time.Duration) {
	w.samples = append(w.samples, s)
	cutoff := s.At.Add(-maxAge)
	idx := 0
	for idx < len(w.samples) && w.samples[idx].At.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		w.samples = append([]Sample(nil), w.samples[idx:]...)
	}
}

func (w *Window) RatioAbove(threshold float64, window time.Duration, now time.Time) float64 {
	return w.ratio(window, now, func(v float64) bool { return v > threshold })
}

func (w *Window) RatioBelow(threshold float64, window time.Duration, now time.Time) float64 {
	return w.ratio(window, now, func(v float64) bool { return v < threshold })
}

func (w *Window) MeetsAbove(threshold float64, window time.Duration, ratio float64, sampleInterval time.Duration, now time.Time) bool {
	return w.ready(window, sampleInterval, now) && w.RatioAbove(threshold, window, now) >= ratio
}

func (w *Window) MeetsBelow(threshold float64, window time.Duration, ratio float64, sampleInterval time.Duration, now time.Time) bool {
	return w.ready(window, sampleInterval, now) && w.RatioBelow(threshold, window, now) >= ratio
}

func (w *Window) ratio(window time.Duration, now time.Time, match func(float64) bool) float64 {
	cutoff := now.Add(-window)
	total := 0
	hit := 0
	for _, sample := range w.samples {
		if sample.At.Before(cutoff) {
			continue
		}
		total++
		if match(sample.CPUPct) {
			hit++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total)
}

func (w *Window) ready(window time.Duration, sampleInterval time.Duration, now time.Time) bool {
	if len(w.samples) == 0 {
		return false
	}
	if sampleInterval <= 0 {
		sampleInterval = time.Second
	}
	cutoff := now.Add(-window)
	total := 0
	var oldest time.Time
	for _, sample := range w.samples {
		if sample.At.Before(cutoff) {
			continue
		}
		if oldest.IsZero() || sample.At.Before(oldest) {
			oldest = sample.At
		}
		total++
	}
	expected := int(window / sampleInterval)
	if expected < 1 {
		expected = 1
	}
	minSamples := expected
	if total < minSamples {
		return false
	}
	return !oldest.After(cutoff.Add(sampleInterval * 2))
}
