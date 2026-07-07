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
	"testing"
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
