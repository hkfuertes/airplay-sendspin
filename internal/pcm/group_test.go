package pcm

import (
	"reflect"
	"testing"
)

func TestGroupSharesChunksByPlaybackTime(t *testing.T) {
	p, err := NewPipeline(8) // 44.1 kHz/16-bit: no resampler, exact samples
	if err != nil {
		t.Fatal(err)
	}
	g := &Group{Pipeline: p}
	defer g.Close()
	p.PushS16([]int16{1, -1, 2, -2})

	at := func(i int64, dst ...int32) []int32 { g.mixAt(dst, i*chunkUs); return dst }
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
	check("drained queue", at(117, 0, 0), 0, 0)
	check("evicted chunk", at(101, 5, 5), 5, 5)
}
