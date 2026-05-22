package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type phase struct {
	name     string
	dutyPct  int
	duration time.Duration
}

func main() {
	var workers int
	var highDuty int
	var highDuration time.Duration
	var recoverDuty int
	var recoverDuration time.Duration
	var period time.Duration
	var quiet bool

	flag.IntVar(&workers, "workers", runtime.NumCPU(), "busy workers; use logical CPU count to approach 100% total CPU")
	flag.IntVar(&highDuty, "duty", 100, "high-load duty cycle percent")
	flag.DurationVar(&highDuration, "duration", 8*time.Minute, "high-load duration")
	flag.IntVar(&recoverDuty, "recover-duty", 5, "recovery phase duty cycle percent")
	flag.DurationVar(&recoverDuration, "recover-duration", 0, "optional recovery phase duration")
	flag.DurationVar(&period, "period", 100*time.Millisecond, "duty-cycle period")
	flag.BoolVar(&quiet, "quiet", false, "suppress periodic status output")
	flag.Parse()

	if workers < 1 {
		workers = 1
	}
	highDuty = clamp(highDuty, 0, 100)
	recoverDuty = clamp(recoverDuty, 0, 100)
	if period < 10*time.Millisecond {
		period = 10 * time.Millisecond
	}

	runtime.GOMAXPROCS(workers)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("cpuguard-burn pid=%d workers=%d gomaxprocs=%d\n", os.Getpid(), workers, runtime.GOMAXPROCS(0))
	fmt.Printf("high phase: duty=%d duration=%s period=%s\n", highDuty, highDuration, period)
	if recoverDuration > 0 {
		fmt.Printf("recovery phase: duty=%d duration=%s\n", recoverDuty, recoverDuration)
	}
	fmt.Println("watch with: cpuguardctl tui | cpuguardctl limits | cpuguardctl logs --type throttled")

	runPhase(ctx, phase{name: "high", dutyPct: highDuty, duration: highDuration}, workers, period, quiet)
	if ctx.Err() == nil && recoverDuration > 0 {
		runPhase(ctx, phase{name: "recover", dutyPct: recoverDuty, duration: recoverDuration}, workers, period, quiet)
	}
	fmt.Println("cpuguard-burn stopped")
}

func runPhase(ctx context.Context, p phase, workers int, period time.Duration, quiet bool) {
	if p.duration <= 0 {
		return
	}
	phaseCtx, cancel := context.WithTimeout(ctx, p.duration)
	defer cancel()

	var iterations atomic.Uint64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			burnWorker(phaseCtx, p.dutyPct, period, &iterations, workerID)
		}(i)
	}

	start := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-phaseCtx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			if !quiet {
				fmt.Printf("phase=%s elapsed=%s duty=%d workers=%d iterations=%d\n",
					p.name,
					time.Since(start).Truncate(time.Second),
					p.dutyPct,
					workers,
					iterations.Load(),
				)
			}
		}
	}
}

func burnWorker(ctx context.Context, dutyPct int, period time.Duration, iterations *atomic.Uint64, workerID int) {
	if dutyPct <= 0 {
		<-ctx.Done()
		return
	}
	busyFor := time.Duration(int64(period) * int64(dutyPct) / 100)
	if busyFor <= 0 {
		busyFor = time.Millisecond
	}
	sleepFor := period - busyFor
	seed := float64(workerID + 1)
	for ctx.Err() == nil {
		start := time.Now()
		for time.Since(start) < busyFor {
			seed = math.Sqrt(seed*1.000001 + 3.14159)
			iterations.Add(1)
		}
		if sleepFor > 0 {
			timer := time.NewTimer(sleepFor)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
	_ = seed
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
