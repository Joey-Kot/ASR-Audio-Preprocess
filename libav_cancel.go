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

//go:build libav

package smartaudio

/*
#include <stdint.h>
*/
import "C"

import (
	"context"
	"runtime/cgo"
)

//export smartaudio_context_canceled
func smartaudio_context_canceled(handle C.uintptr_t) C.int {
	if handle == 0 {
		return 0
	}
	ctx, ok := cgo.Handle(uintptr(handle)).Value().(context.Context)
	if !ok || ctx == nil || ctx.Err() == nil {
		return 0
	}
	return 1
}
