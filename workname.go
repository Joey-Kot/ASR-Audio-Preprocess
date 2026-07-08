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
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var fallbackWorkNameCounter uint64

func newWorkFileStem() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "sa_" + hex.EncodeToString(b[:])
	}
	n := atomic.AddUint64(&fallbackWorkNameCounter, 1)
	binary.BigEndian.PutUint64(b[:], uint64(time.Now().UTC().UnixNano())^n)
	return "sa_" + hex.EncodeToString(b[:])
}
