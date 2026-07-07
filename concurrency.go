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
	"sync"
)

func MapOrdered[T any, R any](ctx context.Context, items []T, workers int, fn func(context.Context, int, T) (R, error)) ([]R, error) {
	if workers <= 0 {
		workers = 1
	}
	if workers > len(items) && len(items) > 0 {
		workers = len(items)
	}
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
