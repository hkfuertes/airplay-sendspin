// Package pcm adapts pushed RAOP PCM to Sendspin's pull-based AudioSource.
package pcm

import (
	"io"
	"sync"
)

const (
	AirPlaySampleRate  = 44100
	SendspinSampleRate = 48000
	SampleRate         = AirPlaySampleRate // default for a direct RAOP source
	Channels           = 2
)

// Sink accepts interleaved S16_LE stereo frames and can discard stale audio.
type Sink interface {
	PushS16([]int16)
	Reset()
}

// Source holds a bounded, interleaved S16_LE PCM stream from an AirPlay 1 receiver.
type Source struct {
	mu         sync.Mutex
	samples    []int32
	head       int
	size       int
	closed     bool
	sampleRate int
	bitDepth   int
}

// New creates a 44.1 kHz source that retains at most bufferFrames stereo frames.
func New(bufferFrames int) *Source {
	return NewAtRate(bufferFrames, SampleRate)
}

// NewAtRate creates a 24-bit source whose Sendspin-facing rate is sampleRate.
func NewAtRate(bufferFrames, sampleRate int) *Source {
	return NewAtFormat(bufferFrames, sampleRate, 24)
}

// NewAtFormat creates a source whose samples match Sendspin's declared format.
func NewAtFormat(bufferFrames, sampleRate, bitDepth int) *Source {
	if bufferFrames < 1 {
		bufferFrames = 1
	}
	if sampleRate < 1 {
		sampleRate = SampleRate
	}
	if bitDepth != 16 && bitDepth != 24 {
		bitDepth = 24
	}
	return &Source{
		samples:    make([]int32, bufferFrames*Channels),
		sampleRate: sampleRate,
		bitDepth:   bitDepth,
	}
}

// PushS16 adds interleaved stereo PCM. When full, the oldest audio is discarded
// so a slow downstream player cannot make latency grow without bound.
func (s *Source) PushS16(pcm []int16) {
	pcm = pcm[:len(pcm)-len(pcm)%Channels]
	if len(pcm) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	capacity := len(s.samples)
	if len(pcm) >= capacity {
		pcm = pcm[len(pcm)-capacity:]
		s.head, s.size = 0, 0
	}

	if overflow := s.size + len(pcm) - capacity; overflow > 0 {
		s.head = (s.head + overflow) % capacity
		s.size -= overflow
	}

	for _, sample := range pcm {
		stored := int32(sample)
		if s.bitDepth > 16 {
			stored <<= s.bitDepth - 16
		}
		s.samples[(s.head+s.size)%capacity] = stored
		s.size++
	}
}

// Read never waits for AirPlay input: missing samples become silence so the
// Sendspin clock remains continuous.
func (s *Source) Read(dst []int32) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.EOF
	}

	n := min(len(dst), s.size)
	for i := range n {
		dst[i] = s.samples[s.head]
		s.head = (s.head + 1) % len(s.samples)
	}
	s.size -= n
	s.mu.Unlock()

	clear(dst[n:])
	return len(dst), nil
}

func (s *Source) SampleRate() int                    { return s.sampleRate }
func (s *Source) Channels() int                      { return Channels }
func (s *Source) Metadata() (string, string, string) { return "", "", "" }

// Reset discards buffered pre-flush audio.
func (s *Source) Reset() {
	s.mu.Lock()
	s.head, s.size = 0, 0
	clear(s.samples)
	s.mu.Unlock()
}

func (s *Source) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}
