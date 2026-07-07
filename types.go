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

import "time"

const (
	DefaultSampleRate          = 16000
	DefaultSilentInterval      = 700 * time.Millisecond
	DefaultPadding             = 100 * time.Millisecond
	DefaultFixedSliceLength    = 5 * time.Second
	DefaultFixedSliceWorkers   = 16
	DefaultMaxSegmentLength    = 175 * time.Second
	DefaultOpusBitrate         = "32k"
	DefaultMinOutputSegmentLen = 10 * time.Millisecond
)

type Config struct {
	Silence   SilenceConfig
	FixedTrim FixedTrimConfig
	Segments  SegmentConfig
}

type SilenceConfig struct {
	MinSilence     time.Duration
	Padding        time.Duration
	ThresholdDB    *float64
	Window         time.Duration
	MinSpeech      time.Duration
	Thresholds     []float64
	ThresholdFloor float64
	ThresholdCeil  float64
}

type FixedTrimConfig struct {
	SliceLength      time.Duration
	Workers          int
	MinSegmentLength time.Duration
	TempDir          string
}

type SegmentConfig struct {
	MaxLength               time.Duration
	SampleRate              int
	OpusBitrate             string
	OutDir                  string
	KeepTempWAV             *bool
	PreserveInternalSilence *bool
}

type Segment struct {
	Index      int
	File       string
	TempWAV    string
	Start      time.Duration
	End        time.Duration
	Cut        time.Duration
	Duration   time.Duration
	Intervals  []Interval
	SourceWAV  string
	SourcePath string
}

type ProcessingInfo struct {
	InputPath  string
	OutputPath string

	InputDuration             time.Duration
	OutputDuration            time.Duration
	DetectedEffectiveDuration time.Duration
	EffectiveDuration         time.Duration

	DetectedSilenceCount        int
	DetectedSpeechIntervalCount int

	FixedSliceCount     int
	FixedSliceSucceeded int
	FixedSliceSkipped   int

	SegmentGroupCount int
	SegmentCount      int
	SegmentSkipped    int
	OutputFiles       []string
}

type VolumeStats struct {
	MeanDB  float64
	MaxDB   float64
	HasMean bool
	HasMax  bool
	Valid   bool
}

type ProbeOrder int

const (
	ProbeMediaFirst ProbeOrder = iota
	ProbeWAVFirst
)

func DefaultConfig() Config {
	return Config{
		Silence: SilenceConfig{
			MinSilence:     DefaultSilentInterval,
			Padding:        DefaultPadding,
			Window:         20 * time.Millisecond,
			MinSpeech:      20 * time.Millisecond,
			ThresholdFloor: -60,
			ThresholdCeil:  -10,
		},
		FixedTrim: FixedTrimConfig{
			SliceLength:      DefaultFixedSliceLength,
			Workers:          DefaultFixedSliceWorkers,
			MinSegmentLength: DefaultMinOutputSegmentLen,
		},
		Segments: SegmentConfig{
			MaxLength:               DefaultMaxSegmentLength,
			SampleRate:              DefaultSampleRate,
			OpusBitrate:             DefaultOpusBitrate,
			KeepTempWAV:             boolPtr(true),
			PreserveInternalSilence: boolPtr(true),
		},
	}
}

func (c Config) normalized() Config {
	d := DefaultConfig()
	if c.Silence.MinSilence > 0 {
		d.Silence.MinSilence = c.Silence.MinSilence
	}
	if c.Silence.Padding > 0 {
		d.Silence.Padding = c.Silence.Padding
	}
	if c.Silence.ThresholdDB != nil {
		d.Silence.ThresholdDB = c.Silence.ThresholdDB
	}
	if c.Silence.Window > 0 {
		d.Silence.Window = c.Silence.Window
	}
	if c.Silence.MinSpeech > 0 {
		d.Silence.MinSpeech = c.Silence.MinSpeech
	}
	if len(c.Silence.Thresholds) > 0 {
		d.Silence.Thresholds = append([]float64(nil), c.Silence.Thresholds...)
	}
	if c.Silence.ThresholdFloor != 0 {
		d.Silence.ThresholdFloor = c.Silence.ThresholdFloor
	}
	if c.Silence.ThresholdCeil != 0 {
		d.Silence.ThresholdCeil = c.Silence.ThresholdCeil
	}
	if c.FixedTrim.SliceLength > 0 {
		d.FixedTrim.SliceLength = c.FixedTrim.SliceLength
	}
	if c.FixedTrim.Workers > 0 {
		d.FixedTrim.Workers = c.FixedTrim.Workers
	}
	if c.FixedTrim.MinSegmentLength > 0 {
		d.FixedTrim.MinSegmentLength = c.FixedTrim.MinSegmentLength
	}
	if c.FixedTrim.TempDir != "" {
		d.FixedTrim.TempDir = c.FixedTrim.TempDir
	}
	if c.Segments.MaxLength > 0 {
		d.Segments.MaxLength = c.Segments.MaxLength
	}
	if c.Segments.SampleRate > 0 {
		d.Segments.SampleRate = c.Segments.SampleRate
	}
	if c.Segments.OpusBitrate != "" {
		d.Segments.OpusBitrate = c.Segments.OpusBitrate
	}
	if c.Segments.OutDir != "" {
		d.Segments.OutDir = c.Segments.OutDir
	}
	if c.Segments.KeepTempWAV != nil {
		d.Segments.KeepTempWAV = c.Segments.KeepTempWAV
	}
	if c.Segments.PreserveInternalSilence != nil {
		d.Segments.PreserveInternalSilence = c.Segments.PreserveInternalSilence
	}
	return d
}

func Bool(v bool) *bool {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}
