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
	"testing"
	"time"
)

func TestInvertAndExpand(t *testing.T) {
	got := InvertAndExpand(
		[]Interval{
			{Start: 1 * time.Second, End: 2 * time.Second},
			{Start: 4 * time.Second, End: 5 * time.Second},
		},
		6*time.Second,
		100*time.Millisecond,
		20*time.Millisecond,
	)
	want := []Interval{
		{Start: 0, End: 1100 * time.Millisecond},
		{Start: 1900 * time.Millisecond, End: 4100 * time.Millisecond},
		{Start: 4900 * time.Millisecond, End: 6 * time.Second},
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interval[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestGroupIntervalsByMaxSpan(t *testing.T) {
	intervals := []Interval{
		{Start: 0, End: 40 * time.Second},
		{Start: 45 * time.Second, End: 70 * time.Second},
		{Start: 80 * time.Second, End: 220 * time.Second},
	}
	got := GroupIntervalsByMaxSpan(intervals, 60*time.Second)
	if len(got) != 5 {
		t.Fatalf("groups=%d want 5: %#v", len(got), got)
	}
	if got[0][0] != intervals[0] {
		t.Fatalf("first group starts with %v", got[0][0])
	}
	if got[1][0] != intervals[1] {
		t.Fatalf("second group starts with %v", got[1][0])
	}
	if got[2][0] != (Interval{Start: 80 * time.Second, End: 140 * time.Second}) {
		t.Fatalf("hard split first piece=%v", got[2][0])
	}
	if got[3][0] != (Interval{Start: 140 * time.Second, End: 200 * time.Second}) {
		t.Fatalf("hard split second piece=%v", got[3][0])
	}
	if got[4][0] != (Interval{Start: 200 * time.Second, End: 220 * time.Second}) {
		t.Fatalf("hard split tail piece=%v", got[4][0])
	}
}

func TestBuildThresholdsFromMaxVolume(t *testing.T) {
	got := BuildThresholds(VolumeStats{MaxDB: -2, MeanDB: -20, HasMax: true, HasMean: true, Valid: true}, SilenceConfig{})
	want := []float64{-20, -18, -16, -14, -12}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("threshold[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestBuildThresholdsFromMeanVolumeWhenMaxMissing(t *testing.T) {
	got := BuildThresholds(VolumeStats{MeanDB: -30, HasMean: true, Valid: true}, SilenceConfig{})
	want := []float64{-38, -32, -26}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("threshold[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestBuildThresholdsFallbackAndDedupe(t *testing.T) {
	got := BuildThresholds(VolumeStats{}, SilenceConfig{})
	want := []float64{-35, -30, -25, -20}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("threshold[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestBuildThresholdsExplicitThresholdIsNotClamped(t *testing.T) {
	threshold := -8.0
	got := BuildThresholds(VolumeStats{}, SilenceConfig{ThresholdDB: &threshold})
	if len(got) != 1 || got[0] != threshold {
		t.Fatalf("got %#v want [%v]", got, threshold)
	}
}

func TestDefaultConfigMatchesPythonMagicNumbers(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Silence.MinSilence != 700*time.Millisecond {
		t.Fatalf("MinSilence=%s", cfg.Silence.MinSilence)
	}
	if cfg.Silence.Padding != 100*time.Millisecond {
		t.Fatalf("Padding=%s", cfg.Silence.Padding)
	}
	if cfg.Silence.MinSpeech != 20*time.Millisecond {
		t.Fatalf("MinSpeech=%s", cfg.Silence.MinSpeech)
	}
	if cfg.FixedTrim.SliceLength != 5*time.Second {
		t.Fatalf("SliceLength=%s", cfg.FixedTrim.SliceLength)
	}
	if cfg.FixedTrim.Workers != 16 {
		t.Fatalf("Workers=%d", cfg.FixedTrim.Workers)
	}
	if cfg.FixedTrim.MinSegmentLength != 10*time.Millisecond {
		t.Fatalf("MinSegmentLength=%s", cfg.FixedTrim.MinSegmentLength)
	}
	if cfg.Libav.CodecThreads != 0 {
		t.Fatalf("CodecThreads=%d", cfg.Libav.CodecThreads)
	}
	if cfg.Segments.MaxLength != 175*time.Second {
		t.Fatalf("MaxLength=%s", cfg.Segments.MaxLength)
	}
	if !boolValue(cfg.Segments.KeepTempWAV, false) {
		t.Fatal("KeepTempWAV default should be true")
	}
	if !boolValue(cfg.Segments.PreserveInternalSilence, false) {
		t.Fatal("PreserveInternalSilence default should be true")
	}
}

func TestLibavCodecThreadsConfigAndOption(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Libav.CodecThreads = 3
	p, err := NewProcessor(WithBackend(&fakeBackend{}), WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config().Libav.CodecThreads != 3 {
		t.Fatalf("CodecThreads=%d want 3", p.Config().Libav.CodecThreads)
	}

	p, err = NewProcessor(WithBackend(&fakeBackend{}), WithLibavCodecThreads(2))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config().Libav.CodecThreads != 2 {
		t.Fatalf("CodecThreads=%d want 2", p.Config().Libav.CodecThreads)
	}
}
