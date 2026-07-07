#include "smartaudio_libav.h"

#include <errno.h>
#include <inttypes.h>
#include <math.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifdef _WIN32
#include <io.h>
#include <windows.h>
#else
#include <glob.h>
#include <pthread.h>
#endif

#include <libavcodec/avcodec.h>
#include <libavfilter/avfilter.h>
#include <libavfilter/buffersink.h>
#include <libavfilter/buffersrc.h>
#include <libavformat/avformat.h>
#include <libavutil/avstring.h>
#include <libavutil/channel_layout.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/mathematics.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>
#include <libavutil/samplefmt.h>

static _Thread_local char sa_error[2048];

__attribute__((constructor)) static void sa_init(void) {
    av_log_set_level(AV_LOG_ERROR);
}

static void sa_set_error(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(sa_error, sizeof(sa_error), fmt, ap);
    va_end(ap);
}

static void sa_set_av_error(const char *op, int err) {
    char buf[AV_ERROR_MAX_STRING_SIZE] = {0};
    av_strerror(err, buf, sizeof(buf));
    sa_set_error("%s: %s", op, buf);
}

const char *sa_last_error(void) {
    return sa_error[0] ? sa_error : "unknown error";
}

typedef struct {
    sa_cancel_cb cb;
    sa_cancel_handle handle;
} sa_cancel;

static int sa_cancelled(const sa_cancel *cancel) {
    if (cancel && cancel->cb && cancel->cb(cancel->handle)) {
        sa_set_error("operation canceled");
        return AVERROR_EXIT;
    }
    return 0;
}

static int sa_interrupt_callback(void *opaque) {
    sa_cancel *cancel = (sa_cancel *)opaque;
    return cancel && cancel->cb && cancel->cb(cancel->handle);
}

void sa_free(void *ptr) {
    av_free(ptr);
}

void sa_free_string_array(char **paths, int path_count) {
    if (!paths) return;
    for (int i = 0; i < path_count; i++) {
        av_free(paths[i]);
    }
    av_free(paths);
}

static int sa_compare_paths(const void *a, const void *b) {
    const char *const *pa = (const char *const *)a;
    const char *const *pb = (const char *const *)b;
    return strcmp(*pa, *pb);
}

static void sa_free_partial_string_array(char **paths, size_t path_count) {
    if (!paths) return;
    for (size_t i = 0; i < path_count; i++) {
        av_free(paths[i]);
    }
    av_free(paths);
}

static int sa_collect_segment_paths(const char *out_dir, const char *filename_prefix, char ***paths, int *path_count) {
    *paths = NULL;
    *path_count = 0;
    char pattern[4096];
    snprintf(pattern, sizeof(pattern), "%s/%s*.wav", out_dir, filename_prefix);

#ifdef _WIN32
    struct _finddata_t entry;
    intptr_t handle = _findfirst(pattern, &entry);
    if (handle == -1) {
        return 0;
    }
    char **out_paths = NULL;
    size_t count = 0;
    size_t capacity = 0;
    do {
        if (entry.attrib & _A_SUBDIR) {
            continue;
        }
        if (count == capacity) {
            size_t next_capacity = capacity ? capacity * 2 : 8;
            char **next_paths = av_realloc_array(out_paths, next_capacity, sizeof(*out_paths));
            if (!next_paths) {
                _findclose(handle);
                sa_free_partial_string_array(out_paths, count);
                return AVERROR(ENOMEM);
            }
            out_paths = next_paths;
            capacity = next_capacity;
        }
        char full_path[4096];
        snprintf(full_path, sizeof(full_path), "%s/%s", out_dir, entry.name);
        out_paths[count] = av_strdup(full_path);
        if (!out_paths[count]) {
            _findclose(handle);
            sa_free_partial_string_array(out_paths, count);
            return AVERROR(ENOMEM);
        }
        count++;
    } while (_findnext(handle, &entry) == 0);
    _findclose(handle);
    if (count == 0) {
        av_free(out_paths);
        return 0;
    }
    qsort(out_paths, count, sizeof(*out_paths), sa_compare_paths);
    *paths = out_paths;
    *path_count = (int)count;
    return 0;
#else
    glob_t g;
    memset(&g, 0, sizeof(g));
    if (glob(pattern, 0, NULL, &g) != 0 || g.gl_pathc == 0) {
        globfree(&g);
        return 0;
    }
    char **out_paths = av_calloc(g.gl_pathc, sizeof(char *));
    if (!out_paths) {
        globfree(&g);
        return AVERROR(ENOMEM);
    }
    for (size_t i = 0; i < g.gl_pathc; i++) {
        out_paths[i] = av_strdup(g.gl_pathv[i]);
        if (!out_paths[i]) {
            globfree(&g);
            sa_free_partial_string_array(out_paths, i);
            return AVERROR(ENOMEM);
        }
    }
    *paths = out_paths;
    *path_count = (int)g.gl_pathc;
    globfree(&g);
    return 0;
	#endif
}

static int sa_remove_segment_paths(const char *out_dir, const char *filename_prefix) {
    char **paths = NULL;
    int path_count = 0;
    int ret = sa_collect_segment_paths(out_dir, filename_prefix, &paths, &path_count);
    if (ret < 0) return ret;
    for (int i = 0; i < path_count; i++) {
        unlink(paths[i]);
    }
    sa_free_string_array(paths, path_count);
    return 0;
}

typedef struct {
    AVFormatContext *fmt;
    AVCodecContext *dec;
    int stream_index;
    AVStream *stream;
} sa_input;

static void sa_close_input(sa_input *in) {
    if (!in) return;
    avcodec_free_context(&in->dec);
    avformat_close_input(&in->fmt);
    in->stream_index = -1;
    in->stream = NULL;
}

static int sa_open_audio_input_ctx(const char *path, sa_input *in, const sa_cancel *cancel) {
    memset(in, 0, sizeof(*in));
    in->stream_index = -1;
    int ret = sa_cancelled(cancel);
    if (ret < 0) return ret;
    in->fmt = avformat_alloc_context();
    if (!in->fmt) {
        sa_set_error("avformat_alloc_context failed");
        return AVERROR(ENOMEM);
    }
    if (cancel && cancel->cb) {
        in->fmt->interrupt_callback.callback = sa_interrupt_callback;
        in->fmt->interrupt_callback.opaque = (void *)cancel;
    }
    ret = avformat_open_input(&in->fmt, path, NULL, NULL);
    if (ret < 0) {
        sa_set_av_error("avformat_open_input", ret);
        return ret;
    }
    ret = sa_cancelled(cancel);
    if (ret < 0) return ret;
    ret = avformat_find_stream_info(in->fmt, NULL);
    if (ret < 0) {
        sa_set_av_error("avformat_find_stream_info", ret);
        return ret;
    }
    ret = sa_cancelled(cancel);
    if (ret < 0) return ret;
    ret = av_find_best_stream(in->fmt, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
    if (ret < 0) {
        sa_set_av_error("av_find_best_stream(audio)", ret);
        return ret;
    }
    in->stream_index = ret;
    in->stream = in->fmt->streams[in->stream_index];
    const AVCodec *decoder = avcodec_find_decoder(in->stream->codecpar->codec_id);
    if (!decoder) {
        sa_set_error("audio decoder not found");
        return AVERROR_DECODER_NOT_FOUND;
    }
    in->dec = avcodec_alloc_context3(decoder);
    if (!in->dec) {
        sa_set_error("avcodec_alloc_context3 failed");
        return AVERROR(ENOMEM);
    }
    ret = avcodec_parameters_to_context(in->dec, in->stream->codecpar);
    if (ret < 0) {
        sa_set_av_error("avcodec_parameters_to_context", ret);
        return ret;
    }
    ret = avcodec_open2(in->dec, decoder, NULL);
    if (ret < 0) {
        sa_set_av_error("avcodec_open2(decoder)", ret);
        return ret;
    }
    return 0;
}

static int sa_open_audio_input(const char *path, sa_input *in) {
    return sa_open_audio_input_ctx(path, in, NULL);
}

static int64_t sa_rescale_to_us(int64_t value, AVRational tb) {
    if (value == AV_NOPTS_VALUE) return AV_NOPTS_VALUE;
    return av_rescale_q(value, tb, AV_TIME_BASE_Q);
}

int sa_probe_duration_ctx(const char *path, int media_first, int64_t *duration_us, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    (void)media_first;
    if (!path || !duration_us) {
        sa_set_error("invalid argument");
        return AVERROR(EINVAL);
    }
    sa_cancel cancel = {cancel_cb, cancel_handle};
    sa_input in;
    int ret = sa_open_audio_input_ctx(path, &in, &cancel);
    if (ret < 0) {
        sa_close_input(&in);
        return ret;
    }
    int64_t duration = AV_NOPTS_VALUE;
    if (in.stream && in.stream->duration != AV_NOPTS_VALUE) {
        duration = sa_rescale_to_us(in.stream->duration, in.stream->time_base);
    }
    if ((duration == AV_NOPTS_VALUE || duration <= 0) && in.fmt->duration != AV_NOPTS_VALUE) {
        duration = in.fmt->duration;
    }
    sa_close_input(&in);
    if (duration == AV_NOPTS_VALUE || duration <= 0) {
        sa_set_error("duration unavailable");
        return AVERROR(EINVAL);
    }
    *duration_us = duration;
    return 0;
}

int sa_probe_duration(const char *path, int media_first, int64_t *duration_us) {
    return sa_probe_duration_ctx(path, media_first, duration_us, NULL, 0);
}

static void sa_describe_input_channel_layout(const AVCodecContext *dec, char *buf, size_t size) {
    if (dec->ch_layout.nb_channels > 0 && av_channel_layout_describe(&dec->ch_layout, buf, size) >= 0) {
        return;
    }
    AVChannelLayout mono;
    av_channel_layout_default(&mono, 1);
    av_channel_layout_describe(&mono, buf, size);
    av_channel_layout_uninit(&mono);
}

typedef struct {
    AVFilterGraph *graph;
    AVFilterContext *src;
    AVFilterContext *sink;
} sa_filter;

static void sa_free_filter(sa_filter *f) {
    if (!f) return;
    avfilter_graph_free(&f->graph);
    f->src = NULL;
    f->sink = NULL;
}

static int sa_init_filter(sa_filter *f, AVCodecContext *dec, const char *desc) {
    memset(f, 0, sizeof(*f));
    char args[512];
    const AVFilter *abuffer = avfilter_get_by_name("abuffer");
    const AVFilter *abuffersink = avfilter_get_by_name("abuffersink");
    if (!abuffer || !abuffersink) {
        sa_set_error("required abuffer/abuffersink filters are unavailable");
        return AVERROR_FILTER_NOT_FOUND;
    }
    f->graph = avfilter_graph_alloc();
    if (!f->graph) {
        sa_set_error("avfilter_graph_alloc failed");
        return AVERROR(ENOMEM);
    }
    AVRational tb = dec->pkt_timebase.num && dec->pkt_timebase.den ? dec->pkt_timebase : (AVRational){1, dec->sample_rate};
    char channel_layout[128];
    sa_describe_input_channel_layout(dec, channel_layout, sizeof(channel_layout));
    snprintf(args, sizeof(args),
             "time_base=%d/%d:sample_rate=%d:sample_fmt=%s:channel_layout=%s",
             tb.num, tb.den, dec->sample_rate, av_get_sample_fmt_name(dec->sample_fmt),
             channel_layout);
    int ret = avfilter_graph_create_filter(&f->src, abuffer, "in", args, NULL, f->graph);
    if (ret < 0) {
        sa_set_av_error("avfilter_graph_create_filter(abuffer)", ret);
        return ret;
    }
    ret = avfilter_graph_create_filter(&f->sink, abuffersink, "out", NULL, NULL, f->graph);
    if (ret < 0) {
        sa_set_av_error("avfilter_graph_create_filter(abuffersink)", ret);
        return ret;
    }
    AVFilterInOut *outputs = avfilter_inout_alloc();
    AVFilterInOut *inputs = avfilter_inout_alloc();
    if (!outputs || !inputs) {
        avfilter_inout_free(&outputs);
        avfilter_inout_free(&inputs);
        sa_set_error("avfilter_inout_alloc failed");
        return AVERROR(ENOMEM);
    }
    outputs->name = av_strdup("in");
    outputs->filter_ctx = f->src;
    outputs->pad_idx = 0;
    outputs->next = NULL;
    inputs->name = av_strdup("out");
    inputs->filter_ctx = f->sink;
    inputs->pad_idx = 0;
    inputs->next = NULL;
    ret = avfilter_graph_parse_ptr(f->graph, desc, &inputs, &outputs, NULL);
    avfilter_inout_free(&inputs);
    avfilter_inout_free(&outputs);
    if (ret < 0) {
        sa_set_av_error("avfilter_graph_parse_ptr", ret);
        return ret;
    }
    ret = avfilter_graph_config(f->graph, NULL);
    if (ret < 0) {
        sa_set_av_error("avfilter_graph_config", ret);
        return ret;
    }
    return 0;
}

typedef int (*sa_frame_cb)(AVFrame *frame, void *opaque);

static int sa_decode_filter_run_ctx(const char *path, const char *filter_desc, sa_frame_cb cb, void *opaque, const sa_cancel *cancel) {
    sa_input in;
    int ret = sa_open_audio_input_ctx(path, &in, cancel);
    if (ret < 0) {
        sa_close_input(&in);
        return ret;
    }
    sa_filter filter;
    ret = sa_init_filter(&filter, in.dec, filter_desc);
    if (ret < 0) {
        sa_free_filter(&filter);
        sa_close_input(&in);
        return ret;
    }
    AVPacket *pkt = av_packet_alloc();
    AVFrame *frame = av_frame_alloc();
    AVFrame *filt = av_frame_alloc();
    if (!pkt || !frame || !filt) {
        ret = AVERROR(ENOMEM);
        sa_set_error("frame/packet allocation failed");
        goto done;
    }
    while ((ret = av_read_frame(in.fmt, pkt)) >= 0) {
        ret = sa_cancelled(cancel);
        if (ret < 0) goto done;
        if (pkt->stream_index != in.stream_index) {
            av_packet_unref(pkt);
            continue;
        }
        ret = avcodec_send_packet(in.dec, pkt);
        av_packet_unref(pkt);
        if (ret < 0) {
            sa_set_av_error("avcodec_send_packet", ret);
            goto done;
        }
        while ((ret = avcodec_receive_frame(in.dec, frame)) >= 0) {
            ret = sa_cancelled(cancel);
            if (ret < 0) goto done;
            ret = av_buffersrc_add_frame_flags(filter.src, frame, AV_BUFFERSRC_FLAG_KEEP_REF);
            av_frame_unref(frame);
            if (ret == AVERROR_EOF) {
                ret = 0;
                goto drain_filter;
            }
            if (ret < 0) {
                sa_set_av_error("av_buffersrc_add_frame_flags", ret);
                goto done;
            }
            while ((ret = av_buffersink_get_frame(filter.sink, filt)) >= 0) {
                ret = sa_cancelled(cancel);
                if (ret < 0) goto done;
                if (cb) {
                    int cbret = cb(filt, opaque);
                    av_frame_unref(filt);
                    if (cbret < 0) {
                        ret = cbret;
                        goto done;
                    }
                } else {
                    av_frame_unref(filt);
                }
            }
            if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) ret = 0;
            if (ret < 0) {
                sa_set_av_error("av_buffersink_get_frame", ret);
                goto done;
            }
        }
        if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) ret = 0;
        if (ret < 0) {
            sa_set_av_error("avcodec_receive_frame", ret);
            goto done;
        }
    }
    if (ret == AVERROR_EOF) ret = 0;
    if (ret < 0) {
        sa_set_av_error("av_read_frame", ret);
        goto done;
    }
    ret = avcodec_send_packet(in.dec, NULL);
    if (ret < 0) {
        sa_set_av_error("avcodec_send_packet(flush)", ret);
        goto done;
    }
    while ((ret = avcodec_receive_frame(in.dec, frame)) >= 0) {
        ret = sa_cancelled(cancel);
        if (ret < 0) goto done;
        ret = av_buffersrc_add_frame_flags(filter.src, frame, AV_BUFFERSRC_FLAG_KEEP_REF);
        av_frame_unref(frame);
        if (ret == AVERROR_EOF) {
            ret = 0;
            goto drain_filter;
        }
        if (ret < 0) {
            sa_set_av_error("av_buffersrc_add_frame_flags(flush)", ret);
            goto done;
        }
    }
drain_filter:
    ret = av_buffersrc_add_frame_flags(filter.src, NULL, 0);
    if (ret == AVERROR_EOF) ret = 0;
    if (ret < 0) {
        sa_set_av_error("av_buffersrc_add_frame_flags(NULL)", ret);
        goto done;
    }
    while ((ret = av_buffersink_get_frame(filter.sink, filt)) >= 0) {
        ret = sa_cancelled(cancel);
        if (ret < 0) goto done;
        if (cb) {
            int cbret = cb(filt, opaque);
            av_frame_unref(filt);
            if (cbret < 0) {
                ret = cbret;
                goto done;
            }
        } else {
            av_frame_unref(filt);
        }
    }
    if (ret == AVERROR_EOF || ret == AVERROR(EAGAIN)) ret = 0;
    if (ret < 0) sa_set_av_error("filter drain", ret);

done:
    av_packet_free(&pkt);
    av_frame_free(&frame);
    av_frame_free(&filt);
    sa_free_filter(&filter);
    sa_close_input(&in);
    return ret;
}

static int sa_decode_filter_run(const char *path, const char *filter_desc, sa_frame_cb cb, void *opaque) {
    return sa_decode_filter_run_ctx(path, filter_desc, cb, opaque, NULL);
}

typedef struct {
    double mean_db;
    double max_db;
    int has_mean;
    int has_max;
} sa_volume_capture;

typedef struct {
    sa_interval *items;
    int count;
    int cap;
    int64_t *starts;
    int start_count;
    int start_cap;
} sa_silence_capture;

static int sa_push_i64(int64_t **items, int *count, int *cap, int64_t value) {
    if (*count >= *cap) {
        int next = *cap ? *cap * 2 : 8;
        int64_t *p = av_realloc_array(*items, next, sizeof(**items));
        if (!p) return AVERROR(ENOMEM);
        *items = p;
        *cap = next;
    }
    (*items)[(*count)++] = value;
    return 0;
}

static int sa_push_interval(sa_silence_capture *cap, int64_t start_us, int64_t end_us) {
    if (cap->count >= cap->cap) {
        int next = cap->cap ? cap->cap * 2 : 8;
        sa_interval *p = av_realloc_array(cap->items, next, sizeof(*cap->items));
        if (!p) return AVERROR(ENOMEM);
        cap->items = p;
        cap->cap = next;
    }
    cap->items[cap->count++] = (sa_interval){start_us, end_us};
    return 0;
}

#ifdef _WIN32
static SRWLOCK sa_log_mutex = SRWLOCK_INIT;

static void sa_log_lock(void) {
    AcquireSRWLockExclusive(&sa_log_mutex);
}

static void sa_log_unlock(void) {
    ReleaseSRWLockExclusive(&sa_log_mutex);
}
#else
static pthread_mutex_t sa_log_mutex = PTHREAD_MUTEX_INITIALIZER;

static void sa_log_lock(void) {
    pthread_mutex_lock(&sa_log_mutex);
}

static void sa_log_unlock(void) {
    pthread_mutex_unlock(&sa_log_mutex);
}
#endif

static sa_volume_capture *sa_current_volume = NULL;
static sa_silence_capture *sa_current_silence = NULL;

static void sa_volume_log_callback(void *ptr, int level, const char *fmt, va_list vl) {
    (void)ptr;
    (void)level;
    char line[1024];
    va_list parse_args;
    va_copy(parse_args, vl);
    vsnprintf(line, sizeof(line), fmt, parse_args);
    va_end(parse_args);
    if (sa_current_volume) {
        double v = 0.0;
        if (sscanf(line, " mean_volume: %lf dB", &v) == 1 || sscanf(line, "mean_volume: %lf dB", &v) == 1) {
            sa_current_volume->mean_db = v;
            sa_current_volume->has_mean = 1;
        }
        if (sscanf(line, " max_volume: %lf dB", &v) == 1 || sscanf(line, "max_volume: %lf dB", &v) == 1) {
            sa_current_volume->max_db = v;
            sa_current_volume->has_max = 1;
        }
    }
}

static void sa_silence_log_callback(void *ptr, int level, const char *fmt, va_list vl) {
    (void)ptr;
    (void)level;
    char line[1024];
    va_list parse_args;
    va_copy(parse_args, vl);
    vsnprintf(line, sizeof(line), fmt, parse_args);
    va_end(parse_args);
    if (!sa_current_silence) return;
    double value = 0.0;
    char *start = strstr(line, "silence_start:");
    if (start && sscanf(start, "silence_start: %lf", &value) == 1) {
        sa_push_i64(&sa_current_silence->starts, &sa_current_silence->start_count, &sa_current_silence->start_cap,
                    (int64_t)llround(value * 1000000.0));
        return;
    }
    char *end = strstr(line, "silence_end:");
    if (end && sscanf(end, "silence_end: %lf", &value) == 1 && sa_current_silence->start_count > 0) {
        int64_t end_us = (int64_t)llround(value * 1000000.0);
        int64_t start_us = sa_current_silence->starts[0];
        memmove(sa_current_silence->starts, sa_current_silence->starts + 1,
                sizeof(int64_t) * (sa_current_silence->start_count - 1));
        sa_current_silence->start_count--;
        sa_push_interval(sa_current_silence, start_us, end_us);
    }
}

int sa_volume_detect_ctx(const char *path, double *mean_db, double *max_db, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (!path || !mean_db || !max_db) {
        sa_set_error("invalid argument");
        return AVERROR(EINVAL);
    }
    sa_cancel cancel = {cancel_cb, cancel_handle};
    sa_volume_capture cap = {0};
    sa_log_lock();
    int old_level = av_log_get_level();
    av_log_set_level(AV_LOG_INFO);
    sa_current_volume = &cap;
    av_log_set_callback(sa_volume_log_callback);
    int ret = sa_decode_filter_run_ctx(path, "volumedetect", NULL, NULL, &cancel);
    av_log_set_callback(av_log_default_callback);
    av_log_set_level(old_level);
    sa_current_volume = NULL;
    sa_log_unlock();
    if (ret < 0) return ret;
    if (!cap.has_mean && !cap.has_max) {
        sa_set_error("volumedetect did not report mean_volume/max_volume");
        return AVERROR(EINVAL);
    }
    *mean_db = cap.has_mean ? cap.mean_db : NAN;
    *max_db = cap.has_max ? cap.max_db : NAN;
    return 0;
}

int sa_volume_detect(const char *path, double *mean_db, double *max_db) {
    return sa_volume_detect_ctx(path, mean_db, max_db, NULL, 0);
}

static int sa_silence_frame_cb(AVFrame *frame, void *opaque) {
    sa_silence_capture *cap = (sa_silence_capture *)opaque;
    AVDictionaryEntry *e = NULL;
    e = av_dict_get(frame->metadata, "lavfi.silence_start", NULL, 0);
    if (e && e->value) {
        double seconds = strtod(e->value, NULL);
        int ret = sa_push_i64(&cap->starts, &cap->start_count, &cap->start_cap, (int64_t)llround(seconds * 1000000.0));
        if (ret < 0) return ret;
    }
    e = av_dict_get(frame->metadata, "lavfi.silence_end", NULL, 0);
    if (e && e->value && cap->start_count > 0) {
        double seconds = strtod(e->value, NULL);
        int64_t end_us = (int64_t)llround(seconds * 1000000.0);
        int64_t start_us = cap->starts[0];
        memmove(cap->starts, cap->starts + 1, sizeof(int64_t) * (cap->start_count - 1));
        cap->start_count--;
        return sa_push_interval(cap, start_us, end_us);
    }
    return 0;
}

int sa_silence_detect_ctx(const char *path, double noise_db, int64_t min_silence_us, sa_interval **intervals, int *interval_count, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (!path || !intervals || !interval_count) {
        sa_set_error("invalid argument");
        return AVERROR(EINVAL);
    }
    *intervals = NULL;
    *interval_count = 0;
    if (min_silence_us < 1000) min_silence_us = 1000;
    char filter[256];
    snprintf(filter, sizeof(filter), "silencedetect=noise=%gdB:d=%.6f", noise_db, (double)min_silence_us / 1000000.0);
    sa_cancel cancel = {cancel_cb, cancel_handle};
    sa_silence_capture cap = {0};
    sa_log_lock();
    int old_level = av_log_get_level();
    av_log_set_level(AV_LOG_INFO);
    sa_current_silence = &cap;
    av_log_set_callback(sa_silence_log_callback);
    int ret = sa_decode_filter_run_ctx(path, filter, NULL, NULL, &cancel);
    av_log_set_callback(av_log_default_callback);
    av_log_set_level(old_level);
    sa_current_silence = NULL;
    sa_log_unlock();
    av_free(cap.starts);
    if (ret < 0) {
        av_free(cap.items);
        return ret;
    }
    *intervals = cap.items;
    *interval_count = cap.count;
    return 0;
}

int sa_silence_detect(const char *path, double noise_db, int64_t min_silence_us, sa_interval **intervals, int *interval_count) {
    return sa_silence_detect_ctx(path, noise_db, min_silence_us, intervals, interval_count, NULL, 0);
}

static int sa_parse_bitrate(const char *bitrate) {
    if (!bitrate || !*bitrate) return 32000;
    char *end = NULL;
    double v = strtod(bitrate, &end);
    if (v <= 0) return 32000;
    if (end && (*end == 'k' || *end == 'K')) v *= 1000.0;
    return (int)v;
}

typedef struct {
    AVFormatContext *fmt;
    AVCodecContext *enc;
    AVStream *stream;
    int64_t next_pts;
} sa_output;

static void sa_close_output(sa_output *out) {
    if (!out) return;
    if (out->fmt) {
        if (!(out->fmt->oformat->flags & AVFMT_NOFILE) && out->fmt->pb) avio_closep(&out->fmt->pb);
        avformat_free_context(out->fmt);
    }
    avcodec_free_context(&out->enc);
}

static int sa_open_audio_output(const char *out_path, const char *format_name, enum AVCodecID codec_id, int sample_rate, int bit_rate, AVDictionary **mux_opts, sa_output *out) {
    memset(out, 0, sizeof(*out));
    int ret = avformat_alloc_output_context2(&out->fmt, NULL, format_name, out_path);
    if (ret < 0 || !out->fmt) {
        sa_set_av_error("avformat_alloc_output_context2", ret);
        return ret < 0 ? ret : AVERROR_UNKNOWN;
    }
    const AVCodec *encoder = avcodec_find_encoder(codec_id);
    if (!encoder) {
        sa_set_error("encoder not found");
        return AVERROR_ENCODER_NOT_FOUND;
    }
    out->stream = avformat_new_stream(out->fmt, NULL);
    if (!out->stream) {
        sa_set_error("avformat_new_stream failed");
        return AVERROR(ENOMEM);
    }
    out->enc = avcodec_alloc_context3(encoder);
    if (!out->enc) {
        sa_set_error("avcodec_alloc_context3(encoder) failed");
        return AVERROR(ENOMEM);
    }
    out->enc->sample_rate = sample_rate > 0 ? sample_rate : 16000;
    out->enc->sample_fmt = encoder->sample_fmts ? encoder->sample_fmts[0] : AV_SAMPLE_FMT_S16;
    av_channel_layout_default(&out->enc->ch_layout, 1);
    out->enc->time_base = (AVRational){1, out->enc->sample_rate};
    if (bit_rate > 0) out->enc->bit_rate = bit_rate;
    if (out->fmt->oformat->flags & AVFMT_GLOBALHEADER) out->enc->flags |= AV_CODEC_FLAG_GLOBAL_HEADER;
    ret = avcodec_open2(out->enc, encoder, NULL);
    if (ret < 0) {
        sa_set_av_error("avcodec_open2(encoder)", ret);
        return ret;
    }
    ret = avcodec_parameters_from_context(out->stream->codecpar, out->enc);
    if (ret < 0) {
        sa_set_av_error("avcodec_parameters_from_context", ret);
        return ret;
    }
    out->stream->time_base = out->enc->time_base;
    if (!(out->fmt->oformat->flags & AVFMT_NOFILE)) {
        ret = avio_open(&out->fmt->pb, out_path, AVIO_FLAG_WRITE);
        if (ret < 0) {
            sa_set_av_error("avio_open", ret);
            return ret;
        }
    }
    ret = avformat_write_header(out->fmt, mux_opts);
    if (ret < 0) {
        sa_set_av_error("avformat_write_header", ret);
        return ret;
    }
    return 0;
}

static int sa_encode_frame_ctx(sa_output *out, AVFrame *frame, const sa_cancel *cancel) {
    int ret;
    ret = sa_cancelled(cancel);
    if (ret < 0) return ret;
    if (frame) {
        frame->pts = out->next_pts;
        out->next_pts += frame->nb_samples;
    }
    ret = avcodec_send_frame(out->enc, frame);
    if (ret < 0) {
        sa_set_av_error("avcodec_send_frame", ret);
        return ret;
    }
    AVPacket *pkt = av_packet_alloc();
    if (!pkt) return AVERROR(ENOMEM);
    while ((ret = avcodec_receive_packet(out->enc, pkt)) >= 0) {
        ret = sa_cancelled(cancel);
        if (ret < 0) {
            av_packet_free(&pkt);
            return ret;
        }
        av_packet_rescale_ts(pkt, out->enc->time_base, out->stream->time_base);
        pkt->stream_index = out->stream->index;
        ret = av_interleaved_write_frame(out->fmt, pkt);
        av_packet_unref(pkt);
        if (ret < 0) {
            sa_set_av_error("av_interleaved_write_frame", ret);
            av_packet_free(&pkt);
            return ret;
        }
    }
    av_packet_free(&pkt);
    if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) return 0;
    sa_set_av_error("avcodec_receive_packet", ret);
    return ret;
}

static int sa_encode_frame(sa_output *out, AVFrame *frame) {
    return sa_encode_frame_ctx(out, frame, NULL);
}

typedef struct {
    sa_output *out;
    const sa_cancel *cancel;
} sa_write_ctx;

static int sa_write_frame_cb(AVFrame *frame, void *opaque) {
    sa_write_ctx *ctx = (sa_write_ctx *)opaque;
    return sa_encode_frame_ctx(ctx->out, frame, ctx->cancel);
}

static const char *sa_sample_fmt_name(enum AVSampleFormat fmt) {
    const char *name = av_get_sample_fmt_name(fmt);
    return name ? name : "s16";
}

static int sa_render_filter_to_output_ctx(const char *input_path, const char *out_path, const char *format_name, enum AVCodecID codec_id, int sample_rate, int bit_rate, const char *filter_head, const sa_cancel *cancel) {
    int ret = sa_cancelled(cancel);
    if (ret < 0) return ret;
    sa_output out;
    ret = sa_open_audio_output(out_path, format_name, codec_id, sample_rate, bit_rate, NULL, &out);
    if (ret < 0) {
        sa_close_output(&out);
        return ret;
    }
    char filter[8192];
    const char *fmt_name = sa_sample_fmt_name(out.enc->sample_fmt);
    if (filter_head && *filter_head) {
        snprintf(filter, sizeof(filter), "%s,aformat=sample_fmts=%s:channel_layouts=mono,aresample=%d", filter_head, fmt_name, out.enc->sample_rate);
    } else {
        snprintf(filter, sizeof(filter), "aformat=sample_fmts=%s:channel_layouts=mono,aresample=%d", fmt_name, out.enc->sample_rate);
    }
    sa_write_ctx wctx = {.out = &out, .cancel = cancel};
    ret = sa_decode_filter_run_ctx(input_path, filter, sa_write_frame_cb, &wctx, cancel);
    if (ret >= 0) ret = sa_encode_frame_ctx(&out, NULL, cancel);
    if (ret >= 0) {
        ret = av_write_trailer(out.fmt);
        if (ret < 0) sa_set_av_error("av_write_trailer", ret);
    }
    sa_close_output(&out);
    return ret;
}

static int sa_render_filter_to_output(const char *input_path, const char *out_path, const char *format_name, enum AVCodecID codec_id, int sample_rate, int bit_rate, const char *filter_head) {
    return sa_render_filter_to_output_ctx(input_path, out_path, format_name, codec_id, sample_rate, bit_rate, filter_head, NULL);
}

int sa_transcode_wav_ctx(const char *input_path, const char *out_path, int sample_rate, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    sa_cancel cancel = {cancel_cb, cancel_handle};
    return sa_render_filter_to_output_ctx(input_path, out_path, "wav", AV_CODEC_ID_PCM_S16LE, sample_rate > 0 ? sample_rate : 16000, 0, NULL, &cancel);
}

int sa_transcode_wav(const char *input_path, const char *out_path, int sample_rate) {
    return sa_transcode_wav_ctx(input_path, out_path, sample_rate, NULL, 0);
}

int sa_export_wav_ctx(const char *input_path, const char *out_path, int64_t start_us, int64_t end_us, int sample_rate, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (end_us <= start_us) {
        sa_set_error("invalid export interval");
        return AVERROR(EINVAL);
    }
    char filter[256];
    snprintf(filter, sizeof(filter), "atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS",
             (double)start_us / 1000000.0, (double)end_us / 1000000.0);
    sa_cancel cancel = {cancel_cb, cancel_handle};
    return sa_render_filter_to_output_ctx(input_path, out_path, "wav", AV_CODEC_ID_PCM_S16LE, sample_rate > 0 ? sample_rate : 16000, 0, filter, &cancel);
}

int sa_export_wav(const char *input_path, const char *out_path, int64_t start_us, int64_t end_us, int sample_rate) {
    return sa_export_wav_ctx(input_path, out_path, start_us, end_us, sample_rate, NULL, 0);
}

int sa_render_intervals_wav_ctx(const char *input_path, const char *out_path, const sa_interval *intervals, int interval_count, int sample_rate, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (!intervals || interval_count <= 0) {
        sa_set_error("interval list is empty");
        return AVERROR(EINVAL);
    }
    sa_cancel cancel = {cancel_cb, cancel_handle};
    char filter[8192];
    size_t used = 0;
    if (interval_count == 1) {
        snprintf(filter, sizeof(filter), "atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS",
                 (double)intervals[0].start_us / 1000000.0,
                 (double)intervals[0].end_us / 1000000.0);
        return sa_render_filter_to_output_ctx(input_path, out_path, "wav", AV_CODEC_ID_PCM_S16LE, sample_rate > 0 ? sample_rate : 16000, 0, filter, &cancel);
    }
    int n = snprintf(filter + used, sizeof(filter) - used, "asplit=outputs=%d", interval_count);
    if (n < 0 || (size_t)n >= sizeof(filter) - used) {
        sa_set_error("filter graph too large");
        return AVERROR(ENOMEM);
    }
    used += (size_t)n;
    for (int i = 0; i < interval_count; i++) {
        n = snprintf(filter + used, sizeof(filter) - used, "[s%d]", i);
        if (n < 0 || (size_t)n >= sizeof(filter) - used) {
            sa_set_error("filter graph too large");
            return AVERROR(ENOMEM);
        }
        used += (size_t)n;
    }
    n = snprintf(filter + used, sizeof(filter) - used, ";");
    if (n < 0 || (size_t)n >= sizeof(filter) - used) {
        sa_set_error("filter graph too large");
        return AVERROR(ENOMEM);
    }
    used += (size_t)n;
    for (int i = 0; i < interval_count; i++) {
        n = snprintf(filter + used, sizeof(filter) - used,
                     "[s%d]atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS[a%d];",
                     i, (double)intervals[i].start_us / 1000000.0,
                     (double)intervals[i].end_us / 1000000.0, i);
        if (n < 0 || (size_t)n >= sizeof(filter) - used) {
            sa_set_error("filter graph too large");
            return AVERROR(ENOMEM);
        }
        used += (size_t)n;
    }
    for (int i = 0; i < interval_count; i++) {
        n = snprintf(filter + used, sizeof(filter) - used, "[a%d]", i);
        if (n < 0 || (size_t)n >= sizeof(filter) - used) {
            sa_set_error("filter graph too large");
            return AVERROR(ENOMEM);
        }
        used += (size_t)n;
    }
    n = snprintf(filter + used, sizeof(filter) - used, "concat=n=%d:v=0:a=1", interval_count);
    if (n < 0 || (size_t)n >= sizeof(filter) - used) {
        sa_set_error("filter graph too large");
        return AVERROR(ENOMEM);
    }
    return sa_render_filter_to_output_ctx(input_path, out_path, "wav", AV_CODEC_ID_PCM_S16LE, sample_rate > 0 ? sample_rate : 16000, 0, filter, &cancel);
}

int sa_render_intervals_wav(const char *input_path, const char *out_path, const sa_interval *intervals, int interval_count, int sample_rate) {
    return sa_render_intervals_wav_ctx(input_path, out_path, intervals, interval_count, sample_rate, NULL, 0);
}

int sa_encode_opus_ctx(const char *wav_path, const char *ogg_path, int sample_rate, const char *bitrate, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    sa_cancel cancel = {cancel_cb, cancel_handle};
    return sa_render_filter_to_output_ctx(wav_path, ogg_path, "ogg", AV_CODEC_ID_OPUS, sample_rate > 0 ? sample_rate : 16000, sa_parse_bitrate(bitrate), NULL, &cancel);
}

int sa_encode_opus(const char *wav_path, const char *ogg_path, int sample_rate, const char *bitrate) {
    return sa_encode_opus_ctx(wav_path, ogg_path, sample_rate, bitrate, NULL, 0);
}

int sa_split_wav_fixed_ctx(const char *wav_path, const char *out_dir, const char *filename_prefix, int64_t slice_us, int sample_rate, char ***paths, int *path_count, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (!wav_path || !out_dir || !filename_prefix || !paths || !path_count) {
        sa_set_error("invalid argument");
        return AVERROR(EINVAL);
    }
    *paths = NULL;
    *path_count = 0;
    sa_cancel cancel = {cancel_cb, cancel_handle};
    int ret = sa_cancelled(&cancel);
    if (ret < 0) return ret;
    ret = sa_remove_segment_paths(out_dir, filename_prefix);
    if (ret < 0) return ret;
    if (slice_us <= 0) slice_us = 5000000;
    char pattern[4096];
    snprintf(pattern, sizeof(pattern), "%s/%s%%04d.wav", out_dir, filename_prefix);
    sa_output out;
    AVDictionary *opts = NULL;
    char segment_time[64];
    snprintf(segment_time, sizeof(segment_time), "%.6f", (double)slice_us / 1000000.0);
    av_dict_set(&opts, "segment_time", segment_time, 0);
    av_dict_set(&opts, "segment_format", "wav", 0);
    av_dict_set(&opts, "reset_timestamps", "1", 0);
    ret = sa_open_audio_output(pattern, "segment", AV_CODEC_ID_PCM_S16LE, sample_rate > 0 ? sample_rate : 16000, 0, &opts, &out);
    av_dict_free(&opts);
    if (ret < 0) {
        sa_close_output(&out);
        return ret;
    }
    sa_write_ctx wctx = {.out = &out, .cancel = &cancel};
    char filter[256];
    snprintf(filter, sizeof(filter), "aformat=sample_fmts=%s:channel_layouts=mono,aresample=%d",
             sa_sample_fmt_name(out.enc->sample_fmt), out.enc->sample_rate);
    ret = sa_decode_filter_run_ctx(wav_path, filter, sa_write_frame_cb, &wctx, &cancel);
    if (ret >= 0) ret = sa_encode_frame_ctx(&out, NULL, &cancel);
    if (ret >= 0) ret = av_write_trailer(out.fmt);
    sa_close_output(&out);
    if (ret < 0) {
        if (!sa_error[0]) sa_set_av_error("segment render", ret);
        return ret;
    }
    return sa_collect_segment_paths(out_dir, filename_prefix, paths, path_count);
}

int sa_split_wav_fixed(const char *wav_path, const char *out_dir, const char *filename_prefix, int64_t slice_us, int sample_rate, char ***paths, int *path_count) {
    return sa_split_wav_fixed_ctx(wav_path, out_dir, filename_prefix, slice_us, sample_rate, paths, path_count, NULL, 0);
}

static int sa_concat_wav_copy_ctx(const char **paths, int path_count, const char *out_path, const sa_cancel *cancel) {
    char list_path[4096];
    snprintf(list_path, sizeof(list_path), "%s.concat_list.txt", out_path);
    FILE *list = fopen(list_path, "w");
    if (!list) {
        sa_set_error("open concat list failed: %s", strerror(errno));
        return AVERROR(errno);
    }
    for (int i = 0; i < path_count; i++) {
        fprintf(list, "file '%s'\n", paths[i]);
    }
    fclose(list);

    AVFormatContext *ifmt = NULL;
    AVFormatContext *ofmt = NULL;
    AVPacket *pkt = NULL;
    int ret;
    int audio_in = -1;
    int audio_out = -1;
    AVDictionary *opts = NULL;
    av_dict_set(&opts, "safe", "0", 0);
    const AVInputFormat *concat_fmt = av_find_input_format("concat");
    ifmt = avformat_alloc_context();
    if (!ifmt) {
        ret = AVERROR(ENOMEM);
        sa_set_error("avformat_alloc_context(concat) failed");
        goto done;
    }
    if (cancel && cancel->cb) {
        ifmt->interrupt_callback.callback = sa_interrupt_callback;
        ifmt->interrupt_callback.opaque = (void *)cancel;
    }
    ret = sa_cancelled(cancel);
    if (ret < 0) goto done;
    ret = avformat_open_input(&ifmt, list_path, concat_fmt, &opts);
    av_dict_free(&opts);
    if (ret < 0) {
        sa_set_av_error("avformat_open_input(concat)", ret);
        goto done;
    }
    ret = avformat_find_stream_info(ifmt, NULL);
    if (ret < 0) {
        sa_set_av_error("avformat_find_stream_info(concat)", ret);
        goto done;
    }
    for (unsigned i = 0; i < ifmt->nb_streams; i++) {
        if (ifmt->streams[i]->codecpar->codec_type == AVMEDIA_TYPE_AUDIO) {
            audio_in = (int)i;
            break;
        }
    }
    if (audio_in < 0) {
        sa_set_error("concat input has no audio stream");
        ret = AVERROR_STREAM_NOT_FOUND;
        goto done;
    }
    ret = avformat_alloc_output_context2(&ofmt, NULL, "wav", out_path);
    if (ret < 0 || !ofmt) {
        sa_set_av_error("avformat_alloc_output_context2(concat)", ret);
        ret = ret < 0 ? ret : AVERROR_UNKNOWN;
        goto done;
    }
    AVStream *out_stream = avformat_new_stream(ofmt, NULL);
    if (!out_stream) {
        sa_set_error("avformat_new_stream(concat) failed");
        ret = AVERROR(ENOMEM);
        goto done;
    }
    audio_out = out_stream->index;
    ret = avcodec_parameters_copy(out_stream->codecpar, ifmt->streams[audio_in]->codecpar);
    if (ret < 0) {
        sa_set_av_error("avcodec_parameters_copy(concat)", ret);
        goto done;
    }
    out_stream->codecpar->codec_tag = 0;
    out_stream->time_base = ifmt->streams[audio_in]->time_base;
    if (!(ofmt->oformat->flags & AVFMT_NOFILE)) {
        ret = avio_open(&ofmt->pb, out_path, AVIO_FLAG_WRITE);
        if (ret < 0) {
            sa_set_av_error("avio_open(concat)", ret);
            goto done;
        }
    }
    ret = avformat_write_header(ofmt, NULL);
    if (ret < 0) {
        sa_set_av_error("avformat_write_header(concat)", ret);
        goto done;
    }
    pkt = av_packet_alloc();
    if (!pkt) {
        ret = AVERROR(ENOMEM);
        goto done;
    }
    while ((ret = av_read_frame(ifmt, pkt)) >= 0) {
        ret = sa_cancelled(cancel);
        if (ret < 0) goto done;
        if (pkt->stream_index != audio_in) {
            av_packet_unref(pkt);
            continue;
        }
        av_packet_rescale_ts(pkt, ifmt->streams[audio_in]->time_base, out_stream->time_base);
        pkt->stream_index = audio_out;
        ret = av_interleaved_write_frame(ofmt, pkt);
        av_packet_unref(pkt);
        if (ret < 0) {
            sa_set_av_error("av_interleaved_write_frame(concat)", ret);
            goto done;
        }
    }
    if (ret == AVERROR_EOF) ret = 0;
    if (ret < 0) {
        sa_set_av_error("av_read_frame(concat)", ret);
        goto done;
    }
    ret = av_write_trailer(ofmt);
    if (ret < 0) {
        sa_set_av_error("av_write_trailer(concat)", ret);
        goto done;
    }

done:
    av_dict_free(&opts);
    av_packet_free(&pkt);
    if (ofmt) {
        if (!(ofmt->oformat->flags & AVFMT_NOFILE) && ofmt->pb) avio_closep(&ofmt->pb);
        avformat_free_context(ofmt);
    }
    avformat_close_input(&ifmt);
    unlink(list_path);
    return ret;
}

static int sa_concat_wav_reencode_ctx(const char **paths, int path_count, const char *out_path, const sa_cancel *cancel) {
    int sample_rate = 16000;
    sa_input first;
    if (sa_open_audio_input_ctx(paths[0], &first, cancel) >= 0) {
        if (first.dec && first.dec->sample_rate > 0) sample_rate = first.dec->sample_rate;
        sa_close_input(&first);
    }
    sa_output out;
    int ret = sa_open_audio_output(out_path, "wav", AV_CODEC_ID_PCM_S16LE, sample_rate, 0, NULL, &out);
    if (ret < 0) {
        sa_close_output(&out);
        return ret;
    }
    for (int i = 0; i < path_count; i++) {
        int cancel_ret = sa_cancelled(cancel);
        if (cancel_ret < 0) {
            ret = cancel_ret;
            break;
        }
        sa_write_ctx wctx = {.out = &out, .cancel = cancel};
        char filter[256];
        snprintf(filter, sizeof(filter), "aformat=sample_fmts=%s:channel_layouts=mono,aresample=%d",
                 sa_sample_fmt_name(out.enc->sample_fmt), out.enc->sample_rate);
        ret = sa_decode_filter_run_ctx(paths[i], filter, sa_write_frame_cb, &wctx, cancel);
        if (ret < 0) break;
    }
    if (ret >= 0) ret = sa_encode_frame_ctx(&out, NULL, cancel);
    if (ret >= 0) {
        ret = av_write_trailer(out.fmt);
        if (ret < 0) sa_set_av_error("av_write_trailer(concat)", ret);
    }
    sa_close_output(&out);
    return ret;
}

int sa_concat_wav_ctx(const char **paths, int path_count, const char *out_path, sa_cancel_cb cancel_cb, sa_cancel_handle cancel_handle) {
    if (!paths || path_count <= 0 || !out_path) {
        sa_set_error("empty concat input");
        return AVERROR(EINVAL);
    }
    sa_cancel cancel = {cancel_cb, cancel_handle};
    int ret = sa_concat_wav_copy_ctx(paths, path_count, out_path, &cancel);
    if (ret == 0) return 0;
    sa_error[0] = '\0';
    return sa_concat_wav_reencode_ctx(paths, path_count, out_path, &cancel);
}

int sa_concat_wav(const char **paths, int path_count, const char *out_path) {
    return sa_concat_wav_ctx(paths, path_count, out_path, NULL, 0);
}
