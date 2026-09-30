package pcm

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// GroupFormat is what every group member receives: one chunk is shared by all
// members, so it is never converted per player.
// ponytail: fixed 48 kHz/16-bit, what the Sendspin players take; adopt the
// members' common format if a player ever lacks it.
var GroupFormat = Format{SampleRate: SendspinSampleRate, BitDepth: 16}

const (
	chunkUs     = 20_000 // Sendspin's fixed chunk; playback times sit on its grid
	groupChunks = 32     // MaxDelay plus the chunk or two member timelines drift apart
)

// MaxDelay is the largest absolute group-audio offset allowed in config.xml.
const MaxDelay = 500 * time.Millisecond

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

// mixAt adds the group audio for the chunk at playbackTime, offset by delay
// samples, to dst. An offset window can straddle two cached chunks.
func (g *Group) mixAt(dst []int32, playbackTime int64, delay int) {
	n := int64(len(dst))
	start := playbackTime/chunkUs*n - int64(delay) // group sample index of dst[0]
	if n == 0 || start < 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for k := start / n; k*n < start+n; k++ {
		chunk := g.chunkAt(k, len(dst))
		if chunk == nil {
			continue
		}
		lo, hi := max(k*n, start), min((k+1)*n, start+n)
		out, in := dst[lo-start:hi-start], chunk[lo-k*n:hi-k*n]
		for j := range out {
			out[j] = min(max(out[j]+in[j], math.MinInt16), math.MaxInt16)
		}
	}
}

// chunkAt returns chunk i, which the first member to ask reads from the queue,
// or nil once it has left the cache. Wants mu.
func (g *Group) chunkAt(i int64, size int) []int32 {
	if g.next == 0 || i-g.next >= groupChunks { // first read, or every member started a new timeline
		g.next = i
	}
	for ; g.next <= i; g.next++ {
		slot := g.next % groupChunks
		if len(g.chunks[slot]) != size {
			g.chunks[slot] = make([]int32, size)
		}
		if _, err := g.Read(g.chunks[slot]); err != nil {
			return nil
		}
		g.index[slot] = g.next
	}
	if slot := i % groupChunks; g.index[slot] == i {
		return g.chunks[slot]
	}
	return nil
}

// Mix is a speaker's Sendspin source: its own AirPlay target plus every group
// it belongs to, summed.
type Mix struct {
	*Pipeline
	groups []*Group
	delay  atomic.Int64 // group audio offset, in samples
}

// SetDelay offsets this speaker's group audio: positive holds it back and
// negative advances it.
func (m *Mix) SetDelay(d time.Duration) {
	frames := int64(d) * int64(GroupFormat.SampleRate) / int64(time.Second)
	m.delay.Store(frames * Channels)
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
			g.mixAt(dst, playbackTime, int(m.delay.Load()))
		}
	}
	return n, err
}

// Grouped reports whether the speaker should prefer GroupFormat.
func (m *Mix) Grouped() bool { return len(m.groups) > 0 }
