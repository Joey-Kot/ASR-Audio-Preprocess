#ifndef SMARTAUDIO_LIBAV_H
#define SMARTAUDIO_LIBAV_H

#include <stdint.h>

typedef struct {
    int64_t start_us;
    int64_t end_us;
} sa_interval;

int sa_probe_duration(const char *path, int media_first, int64_t *duration_us);
int sa_volume_detect(const char *path, double *mean_db, double *max_db);
int sa_silence_detect(const char *path, double noise_db, int64_t min_silence_us, sa_interval **intervals, int *interval_count);
int sa_transcode_wav(const char *input_path, const char *out_path, int sample_rate);
int sa_split_wav_fixed(const char *wav_path, const char *out_dir, const char *filename_prefix, int64_t slice_us, int sample_rate, char ***paths, int *path_count);
int sa_export_wav(const char *input_path, const char *out_path, int64_t start_us, int64_t end_us, int sample_rate);
int sa_render_intervals_wav(const char *input_path, const char *out_path, const sa_interval *intervals, int interval_count, int sample_rate);
int sa_concat_wav(const char **paths, int path_count, const char *out_path);
int sa_encode_opus(const char *wav_path, const char *ogg_path, int sample_rate, const char *bitrate);
const char *sa_last_error(void);
void sa_free(void *ptr);
void sa_free_string_array(char **paths, int path_count);

#endif
