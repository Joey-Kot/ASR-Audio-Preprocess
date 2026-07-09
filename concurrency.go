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
	"sync"
)

func MapOrdered[T any, R any](ctx context.Context, items []T, workers int, fn func(context.Context, int, T) (R, error)) ([]R, error) {
	workers = explicitWorkerCount(len(items), workers)
	type job struct {
		index int
		item  T
	}
	type result struct {
		index int
		value R
		err   error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan job)
	results := make(chan result, len(items))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				value, err := fn(ctx, j.index, j.item)
				if err != nil {
					cancel()
				}
				results <- result{index: j.index, value: value, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, item := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{index: i, item: item}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]R, len(items))
	var firstErr error
	received := 0
	for r := range results {
		received++
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		out[r.index] = r.value
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if received != len(items) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func MapSettledOrdered[T any, R any](ctx context.Context, items []T, workers int, fn func(context.Context, int, T) R) []R {
	workers = effectiveWorkerCount(len(items), workers)
	type job struct {
		index int
		item  T
	}
	type result struct {
		index int
		value R
	}
	jobs := make(chan job)
	results := make(chan result, len(items))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- result{index: j.index, value: fn(ctx, j.index, j.item)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, item := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{index: i, item: item}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]R, len(items))
	for r := range results {
		out[r.index] = r.value
	}
	return out
}

func MapSettledOrderedUntilError[T any, R any](ctx context.Context, items []T, workers int, fn func(context.Context, int, T) (R, error)) ([]R, error) {
	workers = effectiveWorkerCount(len(items), workers)
	type job struct {
		index int
		item  T
	}
	type result struct {
		index int
		value R
		err   error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan job)
	results := make(chan result, len(items))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				value, err := fn(ctx, j.index, j.item)
				if err != nil {
					cancel()
				}
				results <- result{index: j.index, value: value, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, item := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{index: i, item: item}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]R, len(items))
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		out[r.index] = r.value
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, ctx.Err()
}

func effectiveWorkerCount(itemCount, workers int) int {
	if itemCount <= 0 {
		return 1
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > runtime.NumCPU() {
		workers = runtime.NumCPU()
	}
	if workers > itemCount {
		workers = itemCount
	}
	if workers <= 0 {
		return 1
	}
	return workers
}

func explicitWorkerCount(itemCount, workers int) int {
	if workers <= 0 {
		workers = 1
	}
	if workers > itemCount && itemCount > 0 {
		workers = itemCount
	}
	return workers
}
