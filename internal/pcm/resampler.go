package pcm

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/libraop/libcodecs/targets/include/soxr
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../../third_party/libraop/libcodecs/targets/linux/x86_64 -lsoxr -lm
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../../third_party/libraop/libcodecs/targets/linux/aarch64 -lsoxr -lm
#include <soxr.h>
*/
import "C"

import (
	"fmt"
	"log"
	"sync"
	"unsafe"
)

// Resampler converts interleaved S16_LE stereo between sample rates. libsoxr
// keeps its filter state between calls, so 20 ms RAOP packets do not introduce
// a boundary click.
type Resampler struct {
	mu         sync.Mutex
	out        Sink
	inputRate  int
	outputRate int
	soxr       C.soxr_t
	output     []int16
	closed     bool
	failed     bool
}

func NewResampler(inputRate, outputRate int, out Sink) (*Resampler, error) {
	if inputRate < 1 || outputRate < 1 {
		return nil, fmt.Errorf("PCM sample rates must be positive")
	}
	if out == nil {
		return nil, fmt.Errorf("PCM output is required")
	}

	io := C.soxr_io_spec(C.SOXR_INT16_I, C.SOXR_INT16_I)
	quality := C.soxr_quality_spec(C.SOXR_HQ, 0)
	var soxrErr C.soxr_error_t
	soxr := C.soxr_create(
		C.double(inputRate), C.double(outputRate), C.uint(Channels),
		&soxrErr, &io, &quality, nil,
	)
	if soxr == nil {
		detail := "unknown error"
		if soxrErr != nil {
			detail = C.GoString(soxrErr)
		}
		return nil, fmt.Errorf("create PCM resampler: %s", detail)
	}
	return &Resampler{out: out, inputRate: inputRate, outputRate: outputRate, soxr: soxr}, nil
}

// PushS16 converts one or more interleaved S16_LE stereo frames. The caller
// retains pcm; output is copied into the downstream bounded queue before this
// method returns.
func (r *Resampler) PushS16(pcm []int16) {
	frames := len(pcm) / Channels
	if frames == 0 {
		return
	}
	pcm = pcm[:frames*Channels]

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.failed || r.soxr == nil {
		return
	}

	// Extra space covers libsoxr's filter delay and any input it retained when
	// a previous callback filled the output buffer.
	outFrames := (frames*r.outputRate+r.inputRate-1)/r.inputRate + 1024
	if cap(r.output) < outFrames*Channels {
		r.output = make([]int16, outFrames*Channels)
	}
	output := r.output[:outFrames*Channels]

	for offset := 0; offset < frames; {
		var used, written C.size_t
		err := C.soxr_process(
			r.soxr,
			C.soxr_in_t(unsafe.Pointer(&pcm[offset*Channels])), C.size_t(frames-offset), &used,
			C.soxr_out_t(unsafe.Pointer(&output[0])), C.size_t(outFrames), &written,
		)
		if err != nil {
			r.failed = true
			log.Printf("PCM resample: %s", C.GoString(err))
			return
		}
		if written != 0 {
			r.out.PushS16(output[:int(written)*Channels])
		}
		if used == 0 {
			if written == 0 {
				r.failed = true
				log.Printf("PCM resample: libsoxr made no progress")
			}
			return
		}
		offset += int(used)
	}
}

// Reset discards source audio and libsoxr's filter tail at an AirPlay flush.
func (r *Resampler) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.soxr != nil {
		if err := C.soxr_clear(r.soxr); err != nil {
			log.Printf("PCM resample reset: %s", C.GoString(err))
		}
	}
	r.out.Reset()
}

func (r *Resampler) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.soxr != nil {
		C.soxr_delete(r.soxr)
		r.soxr = nil
	}
}
