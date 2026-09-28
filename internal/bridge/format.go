package bridge

import (
	"fmt"
	"strings"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
)

// selectFormat chooses one stereo Sendspin output format for an AirPlay 1
// target. Prefer raw PCM; within that codec, prefer 44.1 kHz to avoid
// resampling and use another declared rate only when needed.
func selectFormat(support *protocol.PlayerV1Support) (pcm.Format, error) {
	if support == nil {
		return pcm.Format{}, fmt.Errorf("player did not announce audio formats")
	}

	var (
		best      pcm.Format
		bestScore = int(^uint(0) >> 1)
	)
	for _, advertised := range advertisedFormats(support) {
		format, ok := usableFormat(advertised)
		if !ok {
			continue
		}
		score := formatScore(format, advertised.Codec)
		if score < bestScore {
			best, bestScore = format, score
		}
	}
	if bestScore == int(^uint(0)>>1) {
		return pcm.Format{}, fmt.Errorf("player has no compatible stereo PCM, FLAC, or Opus format")
	}
	return best, nil
}

func advertisedFormats(support *protocol.PlayerV1Support) []protocol.AudioFormat {
	if len(support.SupportedFormats) != 0 {
		return support.SupportedFormats
	}

	// Old clients advertise independent capability lists. Their Cartesian
	// product is the format contract used by the legacy Sendspin schema.
	var formats []protocol.AudioFormat
	for _, codec := range support.SupportCodecs {
		for _, channels := range support.SupportChannels {
			for _, sampleRate := range support.SupportSampleRates {
				for _, bitDepth := range support.SupportBitDepth {
					formats = append(formats, protocol.AudioFormat{
						Codec: codec, Channels: channels, SampleRate: sampleRate, BitDepth: bitDepth,
					})
				}
			}
		}
	}
	return formats
}

func usableFormat(advertised protocol.AudioFormat) (pcm.Format, bool) {
	// Sendspin emits fixed 20 ms chunks, so the source must have an integral
	// number of frames in 1/50th second.
	if advertised.Channels != pcm.Channels || advertised.SampleRate < 1 || advertised.SampleRate%50 != 0 || (advertised.BitDepth != 16 && advertised.BitDepth != 24) {
		return pcm.Format{}, false
	}
	switch strings.ToLower(advertised.Codec) {
	case "pcm", "flac":
		if advertised.SampleRate > 0 {
			return pcm.Format{SampleRate: advertised.SampleRate, BitDepth: advertised.BitDepth}, true
		}
	case "opus":
		if advertised.SampleRate == 48000 && advertised.BitDepth == 16 {
			return pcm.Format{SampleRate: 48000, BitDepth: 16}, true
		}
	}
	return pcm.Format{}, false
}

func formatScore(format pcm.Format, codec string) int {
	// Direct PCM is the normal bridge path. FLAC and Opus remain valid
	// fallbacks for players that do not advertise PCM.
	score := 0
	switch strings.ToLower(codec) {
	case "pcm":
	case "flac":
		score = 10_000
	default: // Opus
		score = 20_000
	}
	switch format.SampleRate {
	case pcm.AirPlaySampleRate:
	case pcm.SendspinSampleRate:
		score += 100
	default:
		delta := format.SampleRate - pcm.AirPlaySampleRate
		if delta < 0 {
			delta = -delta
		}
		score += 200 + delta/100
	}
	if format.BitDepth == 24 {
		score += 10
	}
	return score
}
