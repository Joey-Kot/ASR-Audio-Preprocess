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

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLibavTrimLongSilencesFromWAV(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "trimmed.wav")
	writeTestWAV(t, input)

	cfg := DefaultConfig()
	cfg.Silence.MinSilence = 700 * time.Millisecond
	cfg.Silence.Padding = 100 * time.Millisecond
	cfg.Segments.SampleRate = 16000
	p, err := NewProcessor(WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	silences, err := p.detectSilences(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(silences) != 2 {
		t.Fatalf("silences=%#v want 2 intervals", silences)
	}
	trimmed, info, err := p.TrimLongSilencesFromWAV(context.Background(), input, output)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed != output {
		t.Fatalf("trimmed path=%q want %q", trimmed, output)
	}
	if info.InputDuration <= 0 {
		t.Fatalf("input duration=%s", info.InputDuration)
	}
	if info.OutputDuration <= 0 {
		t.Fatalf("output duration=%s", info.OutputDuration)
	}
	if info.DetectedSilenceCount != 2 {
		t.Fatalf("detected silence count=%d", info.DetectedSilenceCount)
	}
	origDur, err := p.ProbeDuration(context.Background(), input, ProbeWAVFirst)
	if err != nil {
		t.Fatal(err)
	}
	trimDur, err := p.ProbeDuration(context.Background(), output, ProbeWAVFirst)
	if err != nil {
		t.Fatal(err)
	}
	if trimDur >= origDur {
		t.Fatalf("trimmed duration=%s should be shorter than original=%s", trimDur, origDur)
	}
	if trimDur < 900*time.Millisecond || trimDur > 1500*time.Millisecond {
		t.Fatalf("trimmed duration=%s outside expected range", trimDur)
	}
}

func TestLibavRemoveSilenceByFixedSlicesAndMerge(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "merged.wav")
	writeTestWAV(t, input)

	cfg := DefaultConfig()
	cfg.Silence.MinSilence = 700 * time.Millisecond
	cfg.Silence.Padding = 100 * time.Millisecond
	cfg.FixedTrim.SliceLength = 1 * time.Second
	cfg.FixedTrim.Workers = 2
	cfg.FixedTrim.TempDir = filepath.Join(dir, "work")
	cfg.Segments.SampleRate = 16000
	p, err := NewProcessor(WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	merged, info, err := p.RemoveSilenceByFixedSlicesAndMerge(context.Background(), input, output)
	if err != nil {
		t.Fatal(err)
	}
	if merged != output {
		t.Fatalf("merged path=%q want %q", merged, output)
	}
	if info.FixedSliceCount == 0 {
		t.Fatal("expected fixed slices to be counted")
	}
	if info.FixedSliceSucceeded == 0 {
		t.Fatal("expected successful fixed slices")
	}
	if info.OutputDuration <= 0 {
		t.Fatalf("output duration=%s", info.OutputDuration)
	}
	origDur, err := p.ProbeDuration(context.Background(), input, ProbeWAVFirst)
	if err != nil {
		t.Fatal(err)
	}
	mergedDur, err := p.ProbeDuration(context.Background(), output, ProbeWAVFirst)
	if err != nil {
		t.Fatal(err)
	}
	if mergedDur >= origDur {
		t.Fatalf("merged duration=%s should be shorter than original=%s", mergedDur, origDur)
	}
}

func writeTestWAV(t *testing.T, path string) {
	t.Helper()
	const sampleRate = 16000
	const seconds = 3
	samples := sampleRate * seconds
	dataBytes := samples * 2
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	write := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.Write([]byte("RIFF")); err != nil {
		t.Fatal(err)
	}
	write(uint32(36 + dataBytes))
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		t.Fatal(err)
	}
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	if _, err := f.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	write(uint32(dataBytes))
	for i := 0; i < samples; i++ {
		v := int16(0)
		if i >= sampleRate && i < 2*sampleRate {
			phase := 2 * math.Pi * 440 * float64(i-sampleRate) / sampleRate
			v = int16(math.Sin(phase) * 12000)
		}
		write(v)
	}
}
