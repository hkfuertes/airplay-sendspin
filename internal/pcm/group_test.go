package pcm

import (
	"reflect"
	"testing"
	"time"
)

func TestGroupSharesChunksByPlaybackTime(t *testing.T) {
	p, err := NewPipeline(8) // 44.1 kHz/16-bit: no resampler, exact samples
	if err != nil {
		t.Fatal(err)
	}
	g := &Group{Pipeline: p}
	defer g.Close()
	p.PushS16([]int16{1, -1, 2, -2})

	at := func(i int64, dst ...int32) []int32 { g.mixAt(dst, i*chunkUs, 0); return dst }
	check := func(name string, got []int32, want ...int32) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}

	mix, err := NewMix(8, []*Group{g}) // 44.1 kHz member cannot take GroupFormat
	if err != nil {
		t.Fatal(err)
	}
	defer mix.Close()
	dst := make([]int32, 2)
	if _, err := mix.ReadAt(dst, 100*chunkUs); err != nil {
		t.Fatal(err)
	}
	check("other-format member", dst, 0, 0)

	check("first member", at(100, 10, 10), 11, 9)
	check("second member, same instant", at(100, 0, 0), 1, -1)
	check("clamped mix", at(101, 32767, -32768), 32767, -32768)
	check("drained queue", at(101+groupChunks, 0, 0), 0, 0)
	check("evicted chunk", at(101, 5, 5), 5, 5)
}

func TestGroupDelaysMemberBySamples(t *testing.T) {
	p, err := NewPipeline(8)
	if err != nil {
		t.Fatal(err)
	}
	g := &Group{Pipeline: p}
	defer g.Close()
	p.PushS16([]int16{1, 2, 3, 4, 5, 6, 7, 8}) // two chunks of two stereo frames

	at := func(i int64, delay int) []int32 {
		dst := make([]int32, 4)
		g.mixAt(dst, i*chunkUs, delay)
		return dst
	}
	for _, c := range []struct {
		name string
		got  []int32
		want []int32
	}{
		{"on time", at(100, 0), []int32{1, 2, 3, 4}},
		{"a frame late, before the group began", at(100, 2), []int32{0, 0, 1, 2}},
		{"on time, next chunk", at(101, 0), []int32{5, 6, 7, 8}},
		{"a frame late, across two chunks", at(101, 2), []int32{3, 4, 5, 6}},
		{"a whole chunk late", at(101, 4), []int32{1, 2, 3, 4}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	var m Mix
	m.SetDelay(20 * time.Millisecond) // one 48 kHz stereo chunk
	if got := m.delay.Load(); got != 1920 {
		t.Fatalf("SetDelay(20ms) = %d samples, want 1920", got)
	}
}
