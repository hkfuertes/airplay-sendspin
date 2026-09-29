package pcm

import (
	"math"
	"sync"
)

// GroupFormat is what every group member receives: one chunk is shared by all
// members, so it is never converted per player.
// ponytail: fixed 48 kHz/16-bit, what the Sendspin players take; adopt the
// members' common format if a player ever lacks it.
var GroupFormat = Format{SampleRate: SendspinSampleRate, BitDepth: 16}

const (
	chunkUs     = 20_000 // Sendspin's fixed chunk; playback times sit on its grid
	groupChunks = 16     // member timelines run within a chunk or two of each other
)

// Group is a multiroom AirPlay target. Each 20 ms chunk is read once and kept
// by playback time, so every member gets the same audio for the same instant,
// even through different Sendspin servers (they share a clock and chunk grid).
type Group struct {
	*Pipeline // the group's RAOP sink; members read it through Mix

	mu     sync.Mutex
	chunks [groupChunks][]int32
	index  [groupChunks]int64 // chunk index held by each slot
	next   int64              // next chunk index to read from Pipeline
}

func NewGroup() (*Group, error) {
	p, err := NewPipeline(SendspinSampleRate)
	if err == nil {
		err = p.Configure(GroupFormat)
	}
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	return &Group{Pipeline: p}, nil
}

// mixAt adds the chunk for playbackTime to dst. The first member to ask reads
// it from the queue; a chunk already gone from the cache is left out.
func (g *Group) mixAt(dst []int32, playbackTime int64) {
	i := playbackTime / chunkUs
	g.mu.Lock()
	defer g.mu.Unlock()
	if i-g.next >= groupChunks { // first read, or every member started a new timeline
		g.next = i
	}
	for ; g.next <= i; g.next++ {
		slot := g.next % groupChunks
		if len(g.chunks[slot]) != len(dst) {
			g.chunks[slot] = make([]int32, len(dst))
		}
		if _, err := g.Read(g.chunks[slot]); err != nil {
			return
		}
		g.index[slot] = g.next
	}
	slot := i % groupChunks
	if g.index[slot] != i {
		return
	}
	chunk := g.chunks[slot]
	for j := range min(len(dst), len(chunk)) {
		dst[j] = min(max(dst[j]+chunk[j], math.MinInt16), math.MaxInt16)
	}
}

// Mix is a speaker's Sendspin source: its own AirPlay target plus every group
// it belongs to, summed.
type Mix struct {
	*Pipeline
	groups []*Group
}

func NewMix(bufferFrames int, groups []*Group) (*Mix, error) {
	p, err := NewPipeline(bufferFrames)
	if err != nil {
		return nil, err
	}
	return &Mix{Pipeline: p, groups: groups}, nil
}

// ReadAt implements sendspin.TimedAudioSource. A player that cannot take
// GroupFormat hears only its own AirPlay target.
func (m *Mix) ReadAt(dst []int32, playbackTime int64) (int, error) {
	n, err := m.Read(dst)
	if err == nil && m.Format() == GroupFormat {
		for _, g := range m.groups {
			g.mixAt(dst, playbackTime)
		}
	}
	return n, err
}

// Grouped reports whether the speaker should prefer GroupFormat.
func (m *Mix) Grouped() bool { return len(m.groups) > 0 }
