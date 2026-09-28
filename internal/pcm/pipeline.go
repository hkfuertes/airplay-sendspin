package pcm

import (
	"fmt"
	"io"
	"sync"
)

// Format is the Sendspin-facing output format selected from one player's
// advertised capabilities. AirPlay 1 input is always S16LE stereo at 44.1 kHz.
type Format struct {
	SampleRate int
	BitDepth   int
}

func (f Format) valid() bool {
	return f.SampleRate > 0 && (f.BitDepth == 16 || f.BitDepth == 24)
}

// Pipeline is both libraop's PCM sink and Sendspin's AudioSource. Configure
// swaps its conversion path before the player role reads AudioSource's format.
type Pipeline struct {
	mu           sync.Mutex
	bufferFrames int
	format       Format
	source       *Source
	sink         Sink
	resampler    *Resampler
	closed       bool
}

// NewPipeline starts as native AirPlay PCM. Configure selects the actual
// Sendspin player format after client/hello.
func NewPipeline(bufferFrames int) (*Pipeline, error) {
	p := &Pipeline{bufferFrames: bufferFrames}
	if err := p.configureLocked(Format{SampleRate: AirPlaySampleRate, BitDepth: 16}); err != nil {
		return nil, err
	}
	return p, nil
}

// Configure replaces the queue and resampler atomically. Existing audio is
// deliberately discarded: it belongs to the old stream format or connection.
func (p *Pipeline) Configure(format Format) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return io.EOF
	}
	return p.configureLocked(format)
}

func (p *Pipeline) configureLocked(format Format) error {
	if !format.valid() {
		return fmt.Errorf("unsupported PCM format %d Hz/%d-bit", format.SampleRate, format.BitDepth)
	}

	frames := max(p.bufferFrames, format.SampleRate) // retain at least one second
	source := NewAtFormat(frames, format.SampleRate, format.BitDepth)
	var (
		sink      Sink = source
		resampler *Resampler
		err       error
	)
	if format.SampleRate != AirPlaySampleRate {
		resampler, err = NewResampler(AirPlaySampleRate, format.SampleRate, source)
		if err != nil {
			_ = source.Close()
			return err
		}
		sink = resampler
	}

	oldResampler, oldSource := p.resampler, p.source
	p.format, p.source, p.sink, p.resampler = format, source, sink, resampler
	if oldResampler != nil {
		oldResampler.Close()
	}
	if oldSource != nil {
		_ = oldSource.Close()
	}
	return nil
}

func (p *Pipeline) PushS16(samples []int16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed && p.sink != nil {
		p.sink.PushS16(samples)
	}
}

func (p *Pipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed && p.sink != nil {
		p.sink.Reset()
	}
}

func (p *Pipeline) Read(dst []int32) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.source == nil {
		return 0, io.EOF
	}
	return p.source.Read(dst)
}

func (p *Pipeline) SampleRate() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.format.SampleRate
}

func (p *Pipeline) BitDepth() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.format.BitDepth
}

func (*Pipeline) Channels() int                      { return Channels }
func (*Pipeline) Metadata() (string, string, string) { return "", "", "" }

func (p *Pipeline) Format() Format {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.format
}

func (p *Pipeline) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.resampler != nil {
		p.resampler.Close()
	}
	if p.source != nil {
		return p.source.Close()
	}
	return nil
}
