// Package pcm adapts pushed RAOP PCM to Sendspin's pull-based AudioSource.
package pcm

import (
	"io"
	"log"
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

	pushedSamples uint64
	readSamples   uint64
	silentSamples uint64
	nextPushLog   uint64
	nextReadLog   uint64
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
		samples:     make([]int32, bufferFrames*Channels),
		sampleRate:  sampleRate,
		bitDepth:    bitDepth,
		nextPushLog: uint64(sampleRate * Channels),
		nextReadLog: uint64(sampleRate * Channels),
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

	peak := int32(0)
	for _, sample := range pcm {
		value := int32(sample)
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
		stored := int32(sample)
		if s.bitDepth > 16 {
			stored <<= s.bitDepth - 16
		}
		s.samples[(s.head+s.size)%capacity] = stored
		s.size++
	}
	s.pushedSamples += uint64(len(pcm))
	if s.pushedSamples >= s.nextPushLog {
		log.Printf("[DEBUG-b7c1] PCM push: frames=%d peak=%d queued=%d", s.pushedSamples/Channels, peak, s.size/Channels)
		s.nextPushLog += uint64(s.sampleRate * Channels)
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
	s.readSamples += uint64(n)
	s.silentSamples += uint64(len(dst) - n)
	if s.readSamples+s.silentSamples >= s.nextReadLog {
		log.Printf("[DEBUG-b7c1] PCM pull: frames=%d audio=%d silence=%d queued=%d", (s.readSamples+s.silentSamples)/Channels, s.readSamples/Channels, s.silentSamples/Channels, s.size/Channels)
		s.nextReadLog += uint64(s.sampleRate * Channels)
	}
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
