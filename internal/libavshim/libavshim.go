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

package libavshim

/*
#cgo pkg-config: libavformat libavcodec libavfilter libswresample libavutil
#include <stdint.h>
#include <stdlib.h>
extern int smartaudio_libav_context_canceled(uintptr_t handle);
#include "smartaudio_libav.h"
*/
import "C"

import (
	"context"
	"fmt"
	"runtime"
	"runtime/cgo"
	"time"
	"unsafe"
)

type Interval struct {
	Start time.Duration
	End   time.Duration
}

type VolumeStats struct {
	MeanDB float64
	MaxDB  float64
}

//export smartaudio_libav_context_canceled
func smartaudio_libav_context_canceled(handle C.uintptr_t) C.int {
	if handle == 0 {
		return 0
	}
	ctx, ok := cgo.Handle(uintptr(handle)).Value().(context.Context)
	if !ok || ctx == nil || ctx.Err() == nil {
		return 0
	}
	return 1
}

func cancel(ctx context.Context) (C.sa_cancel_cb, C.sa_cancel_handle, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	handle := cgo.NewHandle(ctx)
	return (C.sa_cancel_cb)(C.smartaudio_libav_context_canceled), C.sa_cancel_handle(uintptr(handle)), func() {
		handle.Delete()
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func cError(op string) error {
	msg := C.sa_last_error()
	if msg == nil {
		return fmt.Errorf("libav %s failed", op)
	}
	return fmt.Errorf("libav %s failed: %s", op, C.GoString(msg))
}

func ProbeDuration(ctx context.Context, path string, mediaFirst bool, codecThreads int) (time.Duration, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()
	var out C.int64_t
	cMediaFirst := C.int(0)
	if mediaFirst {
		cMediaFirst = 1
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_probe_duration_with_threads_ctx(cPath, cMediaFirst, &out, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return 0, err
		}
		return 0, cError("probe duration")
	}
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	return time.Duration(out) * time.Microsecond, nil
}

func VolumeDetect(ctx context.Context, path string, codecThreads int) (VolumeStats, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()
	var mean C.double
	var max C.double

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_volume_detect_with_threads_ctx(cPath, &mean, &max, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return VolumeStats{}, err
		}
		return VolumeStats{}, cError("volumedetect")
	}
	if err := contextErr(ctx); err != nil {
		return VolumeStats{}, err
	}
	return VolumeStats{MeanDB: float64(mean), MaxDB: float64(max)}, nil
}

func SilenceDetect(ctx context.Context, path string, noiseDB float64, minSilence time.Duration, codecThreads int) ([]Interval, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()
	var cIntervals *C.sa_interval
	var cCount C.int

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_silence_detect_with_threads_ctx(cPath, C.double(noiseDB), C.int64_t(minSilence/time.Microsecond), &cIntervals, &cCount, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		return nil, cError("silencedetect")
	}
	defer C.sa_free(unsafe.Pointer(cIntervals))
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
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

func TranscodeToWAV(ctx context.Context, inputPath, wavPath string, sampleRate, codecThreads int) error {
	cInput, cOut := C.CString(inputPath), C.CString(wavPath)
	defer C.free(unsafe.Pointer(cInput))
	defer C.free(unsafe.Pointer(cOut))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_transcode_wav_with_threads_ctx(cInput, cOut, C.int(sampleRate), C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return err
		}
		return cError("transcode wav")
	}
	return contextErr(ctx)
}

func SplitWAVFixed(ctx context.Context, wavPath, outDir, filenamePrefix string, sliceLength time.Duration, sampleRate, codecThreads int) ([]string, error) {
	cWAV, cOutDir, cPrefix := C.CString(wavPath), C.CString(outDir), C.CString(filenamePrefix)
	defer C.free(unsafe.Pointer(cWAV))
	defer C.free(unsafe.Pointer(cOutDir))
	defer C.free(unsafe.Pointer(cPrefix))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()
	var cPaths **C.char
	var cCount C.int

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_split_wav_fixed_with_threads_ctx(cWAV, cOutDir, cPrefix, C.int64_t(sliceLength/time.Microsecond), C.int(sampleRate), &cPaths, &cCount, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		return nil, cError("split wav fixed")
	}
	defer C.sa_free_string_array(cPaths, cCount)
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	count := int(cCount)
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

func ExportWAV(ctx context.Context, inputPath, wavPath string, start, end time.Duration, sampleRate, codecThreads int) error {
	cInput, cOut := C.CString(inputPath), C.CString(wavPath)
	defer C.free(unsafe.Pointer(cInput))
	defer C.free(unsafe.Pointer(cOut))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_export_wav_with_threads_ctx(cInput, cOut, C.int64_t(start/time.Microsecond), C.int64_t(end/time.Microsecond), C.int(sampleRate), C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return err
		}
		return cError("export wav")
	}
	return contextErr(ctx)
}

func RenderIntervalsToWAV(ctx context.Context, inputPath, outWAVPath string, intervals []Interval, sampleRate, codecThreads int) error {
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
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_render_intervals_wav_with_threads_ctx(cInput, cOut, ptr, C.int(len(cIntervals)), C.int(sampleRate), C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return err
		}
		return cError("render intervals wav")
	}
	return contextErr(ctx)
}

func ConcatWAV(ctx context.Context, wavPaths []string, outPath string, codecThreads int) error {
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
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_concat_wav_with_threads_ctx(ptr, C.int(len(cPaths)), cOut, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return err
		}
		return cError("concat wav")
	}
	return contextErr(ctx)
}

func EncodeAudio(ctx context.Context, wavPath, outPath string, sampleRate int, format, codec, bitrate, sampleFormat string, codecThreads int) error {
	cWAV, cOut := C.CString(wavPath), C.CString(outPath)
	cFormat, cCodec, cBitrate, cSampleFormat := C.CString(format), C.CString(codec), C.CString(bitrate), C.CString(sampleFormat)
	defer C.free(unsafe.Pointer(cWAV))
	defer C.free(unsafe.Pointer(cOut))
	defer C.free(unsafe.Pointer(cFormat))
	defer C.free(unsafe.Pointer(cCodec))
	defer C.free(unsafe.Pointer(cBitrate))
	defer C.free(unsafe.Pointer(cSampleFormat))
	cancelCB, cancelHandle, releaseCancel := cancel(ctx)
	defer releaseCancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := C.sa_encode_audio_with_threads_ctx(cWAV, cOut, C.int(sampleRate), cFormat, cCodec, cBitrate, cSampleFormat, C.int(codecThreads), cancelCB, cancelHandle); rc != 0 {
		if err := contextErr(ctx); err != nil {
			return err
		}
		return cError("encode audio")
	}
	return contextErr(ctx)
}
