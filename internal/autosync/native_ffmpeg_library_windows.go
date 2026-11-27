//go:build ffmpeglib && cgo && windows && amd64

package autosync

/*
#cgo LDFLAGS: -lavfilter -lavutil
#include <stdlib.h>
#include <libavfilter/avfilter.h>
#include <libavfilter/buffersink.h>
#include <libavutil/opt.h>
#include <libavutil/frame.h>

typedef struct {
	AVFilterGraph *graph;
	AVFilterContext *sink;
	AVFrame *frame;
} PCMDecoder;

static void pcm_close(PCMDecoder *d) {
	av_frame_free(&d->frame);
	avfilter_graph_free(&d->graph);
}

static int pcm_open(PCMDecoder *d, const char *path) {
	d->graph = avfilter_graph_alloc();
	d->frame = av_frame_alloc();
	if (!d->graph || !d->frame) return AVERROR(ENOMEM);
	d->graph->nb_threads = 1;

	AVFilterContext *source = avfilter_graph_alloc_filter(d->graph, avfilter_get_by_name("amovie"), "input");
	if (!source) return AVERROR_FILTER_NOT_FOUND;
	int err = av_opt_set(source, "filename", path, AV_OPT_SEARCH_CHILDREN);
	if (err < 0) return err;
	err = av_opt_set(source, "streams", "a:0", AV_OPT_SEARCH_CHILDREN);
	if (err < 0) return err;
	err = av_opt_set(source, "dec_threads", "1", AV_OPT_SEARCH_CHILDREN);
	if (err < 0) return err;
	err = av_opt_set(source, "format_opts", "protocol_whitelist=file", AV_OPT_SEARCH_CHILDREN);
	if (err < 0) return err;
	err = avfilter_init_dict(source, NULL);
	if (err < 0) return err;

	AVFilterContext *resample, *format;
	err = avfilter_graph_create_filter(&resample, avfilter_get_by_name("aresample"), "resample", "16000", NULL, d->graph);
	if (err < 0) return err;
	err = avfilter_graph_create_filter(&format, avfilter_get_by_name("aformat"), "format",
		"sample_fmts=s16:channel_layouts=mono:sample_rates=16000", NULL, d->graph);
	if (err < 0) return err;
	err = avfilter_graph_create_filter(&d->sink, avfilter_get_by_name("abuffersink"), "output", NULL, NULL, d->graph);
	if (err < 0) return err;
	if ((err = avfilter_link(source, 0, resample, 0)) < 0) return err;
	if ((err = avfilter_link(resample, 0, format, 0)) < 0) return err;
	if ((err = avfilter_link(format, 0, d->sink, 0)) < 0) return err;
	return avfilter_graph_config(d->graph, NULL);
}

static int pcm_next(PCMDecoder *d) {
	av_frame_unref(d->frame);
	int err = av_buffersink_get_frame(d->sink, d->frame);
	if (err == AVERROR_EOF) return 0;
	if (err < 0) return err;
	return d->frame->nb_samples * 2;
}
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"unsafe"
)

type libraryPCM struct {
	ctx     context.Context
	decoder *C.PCMDecoder
	pending []byte
}

func openLibraryPCM(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	decoder := (*C.PCMDecoder)(C.calloc(1, C.sizeof_PCMDecoder))
	if decoder == nil {
		return nil, fmt.Errorf("allocate FFmpeg decoder")
	}
	reader := &libraryPCM{ctx: ctx, decoder: decoder}
	if code := C.pcm_open(decoder, name); code < 0 {
		reader.Close()
		return nil, fmt.Errorf("open FFmpeg library decoder: error %d", code)
	}
	return reader, nil
}

func (r *libraryPCM) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(r.pending) == 0 {
		count := C.pcm_next(r.decoder)
		if count == 0 {
			return 0, io.EOF
		}
		if count < 0 {
			return 0, fmt.Errorf("read FFmpeg library decoder: error %d", count)
		}
		r.pending = unsafe.Slice((*byte)(unsafe.Pointer(r.decoder.frame.data[0])), int(count))
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *libraryPCM) Close() error {
	C.pcm_close(r.decoder)
	C.free(unsafe.Pointer(r.decoder))
	r.decoder = nil
	r.pending = nil
	return nil
}

func init() {
	openExperimentalPCM = openLibraryPCM
}
