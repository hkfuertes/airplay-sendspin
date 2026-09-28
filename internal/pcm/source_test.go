package pcm

import (
	"io"
	"reflect"
	"testing"
)

func TestSourceConvertsDropsOldAudioAndResets(t *testing.T) {
	s := New(2) // four interleaved samples
	s.PushS16([]int16{1, 2, 3, 4, 5, 6})

	got := make([]int32, 4)
	if n, err := s.Read(got); err != nil || n != len(got) {
		t.Fatalf("Read() = %d, %v; want %d, nil", n, err, len(got))
	}
	want := []int32{3 << 8, 4 << 8, 5 << 8, 6 << 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audio = %v; want %v", got, want)
	}

	s.PushS16([]int16{7, 8})
	s.Reset()
	if _, err := s.Read(got); err != nil {
		t.Fatalf("Read after Reset() = %v", err)
	}
	if !reflect.DeepEqual(got, make([]int32, len(got))) {
		t.Fatalf("reset audio = %v; want silence", got)
	}

	_ = s.Close()
	if _, err := s.Read(got); err != io.EOF {
		t.Fatalf("Read after Close() = %v; want io.EOF", err)
	}
}

func TestSourceKeepsS16ScaleFor16BitSendspin(t *testing.T) {
	s := NewAtFormat(2, SendspinSampleRate, 16)
	s.PushS16([]int16{32767, -32768, 123, -456})

	got := make([]int32, 4)
	if _, err := s.Read(got); err != nil {
		t.Fatal(err)
	}
	want := []int32{32767, -32768, 123, -456}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("16-bit source = %v, want %v", got, want)
	}
}
