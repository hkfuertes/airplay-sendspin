package pcm

import (
	"reflect"
	"testing"
)

func TestPipelineReconfiguresBeforeStreaming(t *testing.T) {
	p, err := NewPipeline(2)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	p.PushS16([]int16{1, 2, 3, 4})
	got := make([]int32, 4)
	if _, err := p.Read(got); err != nil {
		t.Fatal(err)
	}
	if want := []int32{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("initial PCM = %v, want %v", got, want)
	}

	if err := p.Configure(Format{SampleRate: AirPlaySampleRate, BitDepth: 24}); err != nil {
		t.Fatal(err)
	}
	p.PushS16([]int16{1, -2})
	if _, err := p.Read(got[:2]); err != nil {
		t.Fatal(err)
	}
	if want := []int32{1 << 8, -2 << 8}; !reflect.DeepEqual(got[:2], want) {
		t.Fatalf("24-bit PCM = %v, want %v", got[:2], want)
	}

	if err := p.Configure(Format{SampleRate: SendspinSampleRate, BitDepth: 16}); err != nil {
		t.Fatal(err)
	}
	if got := p.Format(); got != (Format{SampleRate: SendspinSampleRate, BitDepth: 16}) {
		t.Fatalf("Format() = %+v", got)
	}
	if got := p.SampleRate(); got != SendspinSampleRate {
		t.Fatalf("SampleRate() = %d, want %d", got, SendspinSampleRate)
	}
}
