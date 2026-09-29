package bridge

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
)

func TestSelectFormat(t *testing.T) {
	for _, tc := range []struct {
		name    string
		support *protocol.PlayerV1Support
		prefer  pcm.Format
		want    pcm.Format
		wantErr bool
	}{
		{
			name: "group format beats native rate",
			support: &protocol.PlayerV1Support{SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", Channels: 2, SampleRate: 44100, BitDepth: 16},
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
			}},
			prefer: pcm.GroupFormat,
			want:   pcm.GroupFormat,
		},
		{
			name: "PCM beats native FLAC",
			support: &protocol.PlayerV1Support{SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
				{Codec: "flac", Channels: 2, SampleRate: 44100, BitDepth: 16},
			}},
			want: pcm.Format{SampleRate: 48000, BitDepth: 16},
		},
		{
			name: "48k fallback",
			support: &protocol.PlayerV1Support{SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", Channels: 2, SampleRate: 48000, BitDepth: 16},
			}},
			want: pcm.Format{SampleRate: 48000, BitDepth: 16},
		},
		{
			name: "legacy capabilities",
			support: &protocol.PlayerV1Support{
				SupportCodecs:      []string{"pcm"},
				SupportChannels:    []int{2},
				SupportSampleRates: []int{44100},
				SupportBitDepth:    []int{24},
			},
			want: pcm.Format{SampleRate: 44100, BitDepth: 24},
		},
		{
			name:    "fractional chunk rate is unsupported",
			support: &protocol.PlayerV1Support{SupportedFormats: []protocol.AudioFormat{{Codec: "pcm", Channels: 2, SampleRate: 11025, BitDepth: 16}}},
			wantErr: true,
		},
		{
			name:    "mono only is unsupported",
			support: &protocol.PlayerV1Support{SupportedFormats: []protocol.AudioFormat{{Codec: "pcm", Channels: 1, SampleRate: 48000, BitDepth: 16}}},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectFormat(tc.support, tc.prefer)
			if (err != nil) != tc.wantErr {
				t.Fatalf("selectFormat() error = %v, want error = %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("selectFormat() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
