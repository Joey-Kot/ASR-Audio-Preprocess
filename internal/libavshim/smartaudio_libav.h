#ifndef SMARTAUDIO_LIBAV_H
#define SMARTAUDIO_LIBAV_H

#include <stdint.h>

typedef struct {
    int64_t start_us;
    int64_t end_us;
} sa_interval;

typedef uintptr_t sa_cancel_handle;
typedef int (*sa_cancel_cb)(sa_cancel_handle handle);

int sa_probe_duration_with_threads_ctx(const char *path, int media_first, int64_t *duration_us, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_volume_detect_with_threads_ctx(const char *path, double *mean_db, double *max_db, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_silence_detect_with_threads_ctx(const char *path, double noise_db, int64_t min_silence_us, sa_interval **intervals, int *interval_count, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_transcode_wav_with_threads_ctx(const char *input_path, const char *out_path, int sample_rate, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_split_wav_fixed_with_threads_ctx(const char *wav_path, const char *out_dir, const char *filename_prefix, int64_t slice_us, int sample_rate, char ***paths, int *path_count, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_export_wav_with_threads_ctx(const char *input_path, const char *out_path, int64_t start_us, int64_t end_us, int sample_rate, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_render_intervals_wav_with_threads_ctx(const char *input_path, const char *out_path, const sa_interval *intervals, int interval_count, int sample_rate, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_concat_wav_with_threads_ctx(const char **paths, int path_count, const char *out_path, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
int sa_encode_audio_with_threads_ctx(const char *wav_path, const char *out_path, int sample_rate, const char *format_name, const char *codec_name, const char *bitrate, const char *sample_format, int codec_threads, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle);
const char *sa_last_error(void);
void sa_free(void *ptr);
void sa_free_string_array(char **paths, int path_count);

#endif
