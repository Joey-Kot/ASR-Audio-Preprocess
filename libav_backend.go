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
#cgo pkg-config: libavformat libavcodec libavfilter libswresample libavutil
#include "internal/libavshim/smartaudio_libav.h"
#include "internal/libavshim/smartaudio_libav.c"
*/
import "C"

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

type LibavBackend struct{}

func defaultBackend() Backend {
	return LibavBackend{}
}

func NewLibavBackend() Backend {
	return LibavBackend{}
}

func (LibavBackend) ProbeDuration(ctx context.Context, path string, order ProbeOrder) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var out C.int64_t
	mediaFirst := C.int(1)
	if order == ProbeWAVFirst {
		mediaFirst = 0
	}
	if rc := C.sa_probe_duration(cPath, mediaFirst, &out); rc != 0 {
		return 0, libavError("probe duration")
	}
	return time.Duration(out) * time.Microsecond, nil
}

func (LibavBackend) VolumeDetect(ctx context.Context, path string) (VolumeStats, error) {
	if err := ctx.Err(); err != nil {
		return VolumeStats{}, err
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var mean C.double
	var max C.double
	if rc := C.sa_volume_detect(cPath, &mean, &max); rc != 0 {
		return VolumeStats{}, libavError("volumedetect")
	}
	return VolumeStats{MeanDB: float64(mean), MaxDB: float64(max), HasMean: true, HasMax: true, Valid: true}, nil
}

func (LibavBackend) SilenceDetect(ctx context.Context, path string, noiseDB float64, minSilence time.Duration) ([]Interval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var cIntervals *C.sa_interval
	var cCount C.int
	if rc := C.sa_silence_detect(cPath, C.double(noiseDB), C.int64_t(minSilence/time.Microsecond), &cIntervals, &cCount); rc != 0 {
		return nil, libavError("silencedetect")
	}
	defer C.sa_free(unsafe.Pointer(cIntervals))
	count := int(cCount)
	if count == 0 {
		return nil, nil
	}
	raw := unsafe.Slice(cIntervals, count)
	out := make([]Interval, 0, count)
	for _, iv := range raw {
		out = append(out, Interval{
			Start: time.Duration(iv.start_us) * time.Microsecond,
			End:   time.Duration(iv.end_us) * time.Microsecond,
		})
	}
	return out, nil
}

func (LibavBackend) TranscodeToWAV(ctx context.Context, inputPath, wavPath string, sampleRate int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cInput, cOut := C.CString(inputPath), C.CString(wavPath)
	defer C.free(unsafe.Pointer(cInput))
	defer C.free(unsafe.Pointer(cOut))
	if rc := C.sa_transcode_wav(cInput, cOut, C.int(sampleRate)); rc != 0 {
		return libavError("transcode wav")
	}
	return nil
}

func (LibavBackend) SplitWAVFixed(ctx context.Context, wavPath, outDir, filenamePrefix string, sliceLength time.Duration, sampleRate int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cWAV, cOutDir, cPrefix := C.CString(wavPath), C.CString(outDir), C.CString(filenamePrefix)
	defer C.free(unsafe.Pointer(cWAV))
	defer C.free(unsafe.Pointer(cOutDir))
	defer C.free(unsafe.Pointer(cPrefix))
	var cPaths **C.char
	var cCount C.int
	if rc := C.sa_split_wav_fixed(cWAV, cOutDir, cPrefix, C.int64_t(sliceLength/time.Microsecond), C.int(sampleRate), &cPaths, &cCount); rc != 0 {
		return nil, libavError("split wav fixed")
	}
	count := int(cCount)
	defer C.sa_free_string_array(cPaths, cCount)
	if count == 0 {
		return nil, nil
	}
	raw := unsafe.Slice(cPaths, count)
	out := make([]string, 0, count)
	for _, path := range raw {
		out = append(out, C.GoString(path))
	}
	return out, nil
}

func (LibavBackend) ExportWAV(ctx context.Context, inputPath, wavPath string, start, end time.Duration, sampleRate int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cInput, cOut := C.CString(inputPath), C.CString(wavPath)
	defer C.free(unsafe.Pointer(cInput))
	defer C.free(unsafe.Pointer(cOut))
	if rc := C.sa_export_wav(cInput, cOut, C.int64_t(start/time.Microsecond), C.int64_t(end/time.Microsecond), C.int(sampleRate)); rc != 0 {
		return libavError("export wav")
	}
	return nil
}

func (LibavBackend) RenderIntervalsToWAV(ctx context.Context, inputPath, outWAVPath string, intervals []Interval, sampleRate int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cInput, cOut := C.CString(inputPath), C.CString(outWAVPath)
	defer C.free(unsafe.Pointer(cInput))
	defer C.free(unsafe.Pointer(cOut))
	cIntervals := make([]C.sa_interval, len(intervals))
	for i, iv := range intervals {
		cIntervals[i] = C.sa_interval{
			start_us: C.int64_t(iv.Start / time.Microsecond),
			end_us:   C.int64_t(iv.End / time.Microsecond),
		}
	}
	var ptr *C.sa_interval
	if len(cIntervals) > 0 {
		ptr = &cIntervals[0]
	}
	if rc := C.sa_render_intervals_wav(cInput, cOut, ptr, C.int(len(cIntervals)), C.int(sampleRate)); rc != 0 {
		return libavError("render intervals wav")
	}
	return nil
}

func (LibavBackend) ConcatWAV(ctx context.Context, wavPaths []string, outPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cOut := C.CString(outPath)
	defer C.free(unsafe.Pointer(cOut))
	cPaths := make([]*C.char, len(wavPaths))
	for i, path := range wavPaths {
		cPaths[i] = C.CString(path)
		defer C.free(unsafe.Pointer(cPaths[i]))
	}
	var ptr **C.char
	if len(cPaths) > 0 {
		ptr = &cPaths[0]
	}
	if rc := C.sa_concat_wav(ptr, C.int(len(cPaths)), cOut); rc != 0 {
		return libavError("concat wav")
	}
	return nil
}

func (LibavBackend) EncodeOpus(ctx context.Context, wavPath, oggPath string, sampleRate int, bitrate string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cWAV, cOGG, cBitrate := C.CString(wavPath), C.CString(oggPath), C.CString(bitrate)
	defer C.free(unsafe.Pointer(cWAV))
	defer C.free(unsafe.Pointer(cOGG))
	defer C.free(unsafe.Pointer(cBitrate))
	if rc := C.sa_encode_opus(cWAV, cOGG, C.int(sampleRate), cBitrate); rc != 0 {
		return libavError("encode opus")
	}
	return nil
}

func libavError(op string) error {
	msg := C.sa_last_error()
	if msg == nil {
		return fmt.Errorf("libav %s failed", op)
	}
	return fmt.Errorf("libav %s failed: %s", op, C.GoString(msg))
}
