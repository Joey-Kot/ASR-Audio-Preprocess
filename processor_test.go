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
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	duration       time.Duration
	splitCalled    bool
	splitPaths     []string
	exportFailures map[string]bool
	renderFailures map[string]bool
	concatInputs   []string
}

func (f *fakeBackend) ProbeDuration(ctx context.Context, path string, order ProbeOrder) (time.Duration, error) {
	return f.duration, nil
}

func (f *fakeBackend) VolumeDetect(ctx context.Context, path string) (VolumeStats, error) {
	return VolumeStats{MeanDB: -30, MaxDB: -2, HasMean: true, HasMax: true, Valid: true}, nil
}

func (f *fakeBackend) SilenceDetect(ctx context.Context, path string, noiseDB float64, minSilence time.Duration) ([]Interval, error) {
	return []Interval{{Start: 1 * time.Second, End: 2 * time.Second}}, nil
}

func (f *fakeBackend) TranscodeToWAV(ctx context.Context, inputPath, wavPath string, sampleRate int) error {
	return nil
}

func (f *fakeBackend) SplitWAVFixed(ctx context.Context, wavPath, outDir, filenamePrefix string, sliceLength time.Duration, sampleRate int) ([]string, error) {
	f.splitCalled = true
	if len(f.splitPaths) > 0 {
		return f.splitPaths, nil
	}
	return []string{
		filepath.Join(outDir, filenamePrefix+"0000.wav"),
		filepath.Join(outDir, filenamePrefix+"0001.wav"),
		filepath.Join(outDir, filenamePrefix+"0002.wav"),
	}, nil
}

func (f *fakeBackend) ExportWAV(ctx context.Context, inputPath, wavPath string, start, end time.Duration, sampleRate int) error {
	for marker := range f.exportFailures {
		if strings.Contains(wavPath, marker) || strings.Contains(inputPath, marker) {
			return errors.New("export failed")
		}
	}
	return nil
}

func (f *fakeBackend) RenderIntervalsToWAV(ctx context.Context, inputPath, outWAVPath string, intervals []Interval, sampleRate int) error {
	for marker := range f.renderFailures {
		if strings.Contains(inputPath, marker) {
			return errors.New("render failed")
		}
	}
	return nil
}

func (f *fakeBackend) ConcatWAV(ctx context.Context, wavPaths []string, outPath string) error {
	f.concatInputs = append([]string(nil), wavPaths...)
	return nil
}

func (f *fakeBackend) EncodeOpus(ctx context.Context, wavPath, oggPath string, sampleRate int, bitrate string) error {
	return nil
}

func TestRemoveSilenceByFixedSlicesUsesBackendSegmentAndSkipsFailedSlices(t *testing.T) {
	backend := &fakeBackend{
		duration:       5 * time.Second,
		renderFailures: map[string]bool{"0001.wav": true},
	}
	cfg := DefaultConfig()
	cfg.FixedTrim.Workers = 2
	cfg.FixedTrim.TempDir = t.TempDir()
	p, err := NewProcessor(WithBackend(backend), WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	out, info, err := p.RemoveSilenceByFixedSlicesAndMerge(context.Background(), "/tmp/input.wav", "/tmp/out.wav")
	if err != nil {
		t.Fatal(err)
	}
	if out != "/tmp/out.wav" {
		t.Fatalf("out=%q", out)
	}
	if !backend.splitCalled {
		t.Fatal("SplitWAVFixed was not called")
	}
	if info.InputDuration != 5*time.Second {
		t.Fatalf("input duration=%s", info.InputDuration)
	}
	if info.FixedSliceCount != 3 {
		t.Fatalf("fixed slice count=%d", info.FixedSliceCount)
	}
	if info.FixedSliceSucceeded != 2 {
		t.Fatalf("fixed slice succeeded=%d", info.FixedSliceSucceeded)
	}
	if info.FixedSliceSkipped != 1 {
		t.Fatalf("fixed slice skipped=%d", info.FixedSliceSkipped)
	}
	if len(backend.concatInputs) != 2 {
		t.Fatalf("concat inputs=%#v want 2 successful slices", backend.concatInputs)
	}
	if !strings.Contains(backend.concatInputs[0], "slice_trim0001.wav") {
		t.Fatalf("first concat input=%q", backend.concatInputs[0])
	}
	if !strings.Contains(backend.concatInputs[1], "slice_trim0003.wav") {
		t.Fatalf("second concat input=%q", backend.concatInputs[1])
	}
}

func TestSplitWAVBySilenceGroupsSkipsFailedSegmentExports(t *testing.T) {
	backend := &fakeBackend{
		duration:       10 * time.Second,
		exportFailures: map[string]bool{"seg001": true},
	}
	cfg := DefaultConfig()
	cfg.Silence.Padding = 0
	cfg.Segments.MaxLength = 3 * time.Second
	cfg.Segments.OutDir = t.TempDir()
	p, err := NewProcessor(WithBackend(backend), WithConfig(cfg), WithSilencePadding(0))
	if err != nil {
		t.Fatal(err)
	}
	segments, info, err := p.SplitWAVBySilenceGroups(context.Background(), "/tmp/input.wav")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) == 0 {
		t.Fatal("expected later segments to be kept after first export failure")
	}
	for _, segment := range segments {
		if segment.Index == 1 {
			t.Fatalf("failed segment should have been skipped: %#v", segments)
		}
	}
	if info.SegmentGroupCount == 0 {
		t.Fatal("expected segment groups to be counted")
	}
	if info.SegmentCount != len(segments) {
		t.Fatalf("segment count=%d len=%d", info.SegmentCount, len(segments))
	}
	if info.SegmentSkipped != 1 {
		t.Fatalf("segment skipped=%d", info.SegmentSkipped)
	}
}

func TestWithSilencePaddingAllowsZero(t *testing.T) {
	p, err := NewProcessor(WithBackend(&fakeBackend{}), WithSilencePadding(0))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config().Silence.Padding != 0 {
		t.Fatalf("padding=%s want 0", p.Config().Silence.Padding)
	}
}
