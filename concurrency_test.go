// Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed WITHOUT ANY WARRANTY; without even the
// implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
// See <https://www.gnu.org/licenses/> for more details.

package smartaudio

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestMapOrderedPreservesInputOrder(t *testing.T) {
	items := []int{1, 2, 3, 4}
	got, err := MapOrdered(context.Background(), items, 2, func(ctx context.Context, index int, value int) (int, error) {
		return value * 10, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{10, 20, 30, 40}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%d want %d", i, got[i], want[i])
		}
	}
}

func TestMapOrderedZeroWorkersRunsSerially(t *testing.T) {
	items := []int{1, 2, 3}
	var active int32
	var maxActive int32
	_, err := MapOrdered(context.Background(), items, 0, func(ctx context.Context, index int, value int) (int, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			seen := atomic.LoadInt32(&maxActive)
			if current <= seen || atomic.CompareAndSwapInt32(&maxActive, seen, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&active, -1)
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxActive != 1 {
		t.Fatalf("max active workers=%d want 1", maxActive)
	}
}

func TestMapSettledOrderedPreservesInputOrder(t *testing.T) {
	items := []int{1, 2, 3, 4}
	got := MapSettledOrdered(context.Background(), items, 2, func(ctx context.Context, index int, value int) int {
		return value * 10
	})
	want := []int{10, 20, 30, 40}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%d want %d", i, got[i], want[i])
		}
	}
}

func TestEffectiveWorkerCountClampsToCPUAndItems(t *testing.T) {
	cpus := runtime.NumCPU()
	if got := effectiveWorkerCount(0, 0); got != 1 {
		t.Fatalf("empty workers=%d want 1", got)
	}
	wantWorkers := min(2, cpus)
	if got := effectiveWorkerCount(2, 99); got != wantWorkers {
		t.Fatalf("workers=%d want %d", got, wantWorkers)
	}
	if got := effectiveWorkerCount(cpus+10, cpus+10); got != cpus {
		t.Fatalf("workers=%d want %d", got, cpus)
	}
	if got := effectiveWorkerCount(cpus+10, 0); got != cpus {
		t.Fatalf("auto workers=%d want %d", got, cpus)
	}
}
