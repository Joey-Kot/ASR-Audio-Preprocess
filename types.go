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
	"strings"
	"time"
)

const (
	DefaultSilentInterval      = 700 * time.Millisecond
	DefaultPadding             = 100 * time.Millisecond
	DefaultFixedSliceLength    = 5 * time.Second
	DefaultFixedSliceWorkers   = 16
	DefaultSegmentWorkers      = 0
	DefaultLibavCodecThreads   = 0
	DefaultMaxSegmentLength    = 175 * time.Second
	DefaultOutputSampleRate    = 16000
	DefaultOutputFormat        = "ogg"
	DefaultOutputCodec         = "libopus"
	DefaultOutputBitrate       = "32k"
	DefaultOutputSampleFormat  = "s16"
	DefaultMinOutputSegmentLen = 10 * time.Millisecond
)

type Config struct {
	Silence   SilenceConfig
	FixedTrim FixedTrimConfig
	Segments  SegmentConfig
	Libav     LibavConfig
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
	Workers                 int
	OutputSampleRate        int
	OutputFormat            string
	OutputCodec             string
	OutputBitrate           string
	OutputSampleFormat      string
	OutDir                  string
	KeepTempWAV             *bool
	PreserveInternalSilence *bool
}

type LibavConfig struct {
	CodecThreads int
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
			Workers:                 DefaultSegmentWorkers,
			OutputSampleRate:        DefaultOutputSampleRate,
			OutputFormat:            DefaultOutputFormat,
			OutputCodec:             DefaultOutputCodec,
			OutputBitrate:           DefaultOutputBitrate,
			OutputSampleFormat:      DefaultOutputSampleFormat,
			KeepTempWAV:             boolPtr(true),
			PreserveInternalSilence: boolPtr(true),
		},
		Libav: LibavConfig{
			CodecThreads: DefaultLibavCodecThreads,
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
	if c.Segments.Workers >= 0 {
		d.Segments.Workers = c.Segments.Workers
	}
	if c.Segments.OutputSampleRate > 0 {
		d.Segments.OutputSampleRate = c.Segments.OutputSampleRate
	}
	if c.Segments.OutputFormat != "" {
		d.Segments.OutputFormat = normalizeAudioFormat(c.Segments.OutputFormat)
	}
	if c.Segments.OutputCodec != "" {
		d.Segments.OutputCodec = normalizeAudioCodec(c.Segments.OutputCodec)
	}
	if c.Segments.OutputBitrate != "" {
		d.Segments.OutputBitrate = strings.TrimSpace(c.Segments.OutputBitrate)
	}
	if c.Segments.OutputSampleFormat != "" {
		d.Segments.OutputSampleFormat = normalizeAudioSampleFormat(c.Segments.OutputSampleFormat)
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
	if c.Libav.CodecThreads >= 0 {
		d.Libav.CodecThreads = c.Libav.CodecThreads
	}
	return d
}

func Bool(v bool) *bool {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func (c SegmentConfig) audioOutput() audioOutputConfig {
	format := normalizeAudioFormat(c.OutputFormat)
	if format == "" {
		format = DefaultOutputFormat
	}
	codec := normalizeAudioCodec(c.OutputCodec)
	if codec == "" {
		codec = defaultCodecForFormat(format)
	}
	if format != DefaultOutputFormat && codec == DefaultOutputCodec {
		codec = defaultCodecForFormat(format)
	}
	sampleFormat := normalizeAudioSampleFormat(c.OutputSampleFormat)
	if sampleFormat == "" {
		sampleFormat = DefaultOutputSampleFormat
	}
	if format == "wav" && codec == "pcm_s16le" && sampleFormat != "" {
		codec = codecForWAVSampleFormat(sampleFormat)
	}
	bitrate := strings.TrimSpace(c.OutputBitrate)
	if bitrate == "" {
		bitrate = DefaultOutputBitrate
	}
	if !codecUsesBitrate(codec) {
		bitrate = ""
	}
	return audioOutputConfig{
		Format:       format,
		Codec:        codec,
		Bitrate:      bitrate,
		SampleFormat: sampleFormat,
		Extension:    extensionForFormat(format, codec),
	}
}

func (c SegmentConfig) allowEncodeFallbackToWAV(output audioOutputConfig) bool {
	return output.Format == DefaultOutputFormat &&
		output.Codec == DefaultOutputCodec &&
		output.Bitrate == DefaultOutputBitrate &&
		output.SampleFormat == DefaultOutputSampleFormat &&
		strings.TrimSpace(c.OutputBitrate) == DefaultOutputBitrate
}

type audioOutputConfig struct {
	Format       string
	Codec        string
	Bitrate      string
	SampleFormat string
	Extension    string
}

func normalizeAudioFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	format = strings.TrimPrefix(format, ".")
	switch format {
	case "aac":
		return "adts"
	case "m4a":
		return "mp4"
	case "opus":
		return "ogg"
	default:
		return format
	}
}

func normalizeAudioCodec(codec string) string {
	return strings.ToLower(strings.TrimSpace(codec))
}

func normalizeAudioSampleFormat(sampleFormat string) string {
	sampleFormat = strings.ToLower(strings.TrimSpace(sampleFormat))
	switch sampleFormat {
	case "16", "16bit", "int16", "pcm_s16le":
		return "s16"
	case "24", "24bit", "int24", "pcm_s24le":
		return "s24"
	case "32", "32bit", "int32", "pcm_s32le":
		return "s32"
	case "float", "float32", "f32", "flt", "pcm_f32le":
		return "f32"
	default:
		return sampleFormat
	}
}

func codecForWAVSampleFormat(sampleFormat string) string {
	switch normalizeAudioSampleFormat(sampleFormat) {
	case "s24":
		return "pcm_s24le"
	case "s32":
		return "pcm_s32le"
	case "f32":
		return "pcm_f32le"
	default:
		return "pcm_s16le"
	}
}

func defaultCodecForFormat(format string) string {
	switch normalizeAudioFormat(format) {
	case "wav":
		return "pcm_s16le"
	case "flac":
		return "flac"
	case "adts", "mp4":
		return "aac"
	default:
		return DefaultOutputCodec
	}
}

func extensionForFormat(format, codec string) string {
	switch normalizeAudioFormat(format) {
	case "wav":
		return "wav"
	case "flac":
		return "flac"
	case "adts":
		return "aac"
	case "mp4":
		return "m4a"
	case "ogg":
		if strings.EqualFold(codec, "opus") {
			return "ogg"
		}
		return "ogg"
	default:
		format = strings.TrimPrefix(strings.TrimSpace(format), ".")
		if format == "" {
			return DefaultOutputFormat
		}
		return format
	}
}

func codecUsesBitrate(codec string) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	switch codec {
	case "libopus", "opus", "aac":
		return true
	default:
		return false
	}
}
