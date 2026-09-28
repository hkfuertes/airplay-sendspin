package pcm

import "testing"

type captureSink struct {
	pcm    []int16
	resets int
}

func (s *captureSink) PushS16(pcm []int16) { s.pcm = append(s.pcm, pcm...) }
func (s *captureSink) Reset()              { s.pcm, s.resets = nil, s.resets+1 }

func TestResamplerProducesContinuous48kStereo(t *testing.T) {
	out := &captureSink{}
	r, err := NewResampler(AirPlaySampleRate, SendspinSampleRate, out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	const seconds = 3
	input := make([]int16, AirPlaySampleRate*seconds*Channels)
	for frame := 0; frame < AirPlaySampleRate*seconds; frame++ {
		// A non-zero repeating waveform proves samples, not just timing, cross C.
		v := int16((frame%251)*260 - 32000)
		input[frame*Channels], input[frame*Channels+1] = v, v
	}
	for offset := 0; offset < len(input); {
		end := min(offset+882*Channels, len(input)) // normal 20 ms RAOP chunks
		r.PushS16(input[offset:end])
		offset = end
	}

	gotFrames := len(out.pcm) / Channels
	wantFrames := SendspinSampleRate * seconds
	// The undrained final libsoxr filter tail is intentionally absent in a
	// live stream, but must be small compared with three seconds of audio.
	if gotFrames < wantFrames-2000 || gotFrames > wantFrames {
		t.Fatalf("resampled frames = %d, want about %d", gotFrames, wantFrames)
	}
	peak := 0
	for _, sample := range out.pcm {
		value := int(sample)
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	if peak < 20_000 {
		t.Fatalf("resampled PCM peak = %d, want >= 20000", peak)
	}

	r.Reset()
	if out.resets != 1 || len(out.pcm) != 0 {
		t.Fatalf("Reset() = %d resets, %d samples; want 1, 0", out.resets, len(out.pcm))
	}
}
