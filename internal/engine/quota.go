package engine

import "math"

const cpuPeriodMicros = 100000

func CalculateLimit(observedPct, factor, minPct float64) float64 {
	limit := observedPct * factor
	if limit < minPct {
		limit = minPct
	}
	return limit
}

func LimitToCPUQuota(limitPct float64) int64 {
	if limitPct <= 0 {
		return cpuPeriodMicros
	}
	return int64(math.Round((limitPct / 100.0) * cpuPeriodMicros))
}
