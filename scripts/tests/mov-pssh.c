/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Minimal MOV tracks exercise pssh parsing without requiring a media fixture. */
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <libavformat/avformat.h>
#include <libavutil/avassert.h>
#include <libavutil/encryption_info.h>
#include <libavutil/intreadwrite.h>

static void u32(FILE *f, uint32_t value) {
    uint8_t bytes[4];
    AV_WB32(bytes, value);
    av_assert0(fwrite(bytes, 1, sizeof(bytes), f) == sizeof(bytes));
}

static void atom(FILE *f, const char *type, uint32_t size) {
    u32(f, size);
    av_assert0(fwrite(type, 1, 4, f) == 4);
}

int main(int argc, char **argv) {
    const unsigned counts[] = {32768, 7};
    AVFormatContext *fmt = NULL;
    FILE *f;
    av_assert0(argc == 2);
    f = fopen(argv[1], "wb");
    av_assert0(f);
    atom(f, "moov", 8 + 16 + 36 * (counts[0] + counts[1]));
    for (unsigned track = 0; track < 2; track++) {
        atom(f, "trak", 8 + 36 * counts[track]);
        for (unsigned i = 0; i < counts[track]; i++) {
            uint8_t system_id[16] = {0};
            system_id[0] = track;
            atom(f, "pssh", 36);
            u32(f, 0);
            av_assert0(fwrite(system_id, 1, 16, f) == 16);
            u32(f, 4);
            u32(f, i);
        }
    }
    av_assert0(fclose(f) == 0);
    av_assert0(avformat_open_input(&fmt, argv[1], NULL, NULL) == 0);
    av_assert0(fmt->nb_streams == 2);
    for (unsigned track = 0; track < 2; track++) {
        AVCodecParameters *par = fmt->streams[track]->codecpar;
        const AVPacketSideData *side = av_packet_side_data_get(
            par->coded_side_data, par->nb_coded_side_data,
            AV_PKT_DATA_ENCRYPTION_INIT_INFO);
        AVEncryptionInitInfo *head, *cur;
        av_assert0(side && AV_RB32(side->data) == counts[track]);
        head = cur = av_encryption_init_info_get_side_data(side->data, side->size);
        for (unsigned i = 0; i < counts[track]; i++, cur = cur->next) {
            av_assert0(cur && cur->system_id_size == 16);
            av_assert0(cur->system_id[0] == track);
            av_assert0(cur->data_size == 4 && AV_RB32(cur->data) == i);
        }
        av_assert0(!cur);
        av_encryption_init_info_free(head);
    }
    avformat_close_input(&fmt);
    puts("MOV pssh regression passed: 32,768 + 7 ordered records across two tracks");
    return 0;
}
