package bridge

import (
	"math"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/sendspin"
	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
)

type testVolumeControl struct {
	clientID string
	volume   int
}

func (c *testVolumeControl) SetVolume(clientID string, volume int) error {
	c.clientID, c.volume = clientID, volume
	return nil
}

// Clients reports the last volume set, like a player echoing its state.
func (c *testVolumeControl) Clients() []sendspin.ClientInfo {
	return []sendspin.ClientInfo{{ID: c.clientID, Volume: c.volume}}
}

func TestGroupVolumeKeepsDifferences(t *testing.T) {
	for _, test := range []struct {
		name          string
		volume        float64
		before, after [2]int
	}{
		{"shift", 0.6, [2]int{40, 60}, [2]int{50, 70}},
		{"clamp top", 0.9, [2]int{90, 50}, [2]int{100, 80}},
		{"clamp bottom", 0.1, [2]int{10, 50}, [2]int{0, 20}},
		{"max", 1, [2]int{40, 60}, [2]int{100, 100}},
	} {
		// "off" has no Sendspin session, so it must not count toward the mean.
		m := &Manager{targets: map[string]*target{"off": {}}}
		members := []groupMember{{ID: "off"}}
		var controls [2]*testVolumeControl
		for i, id := range []string{"dot", "show"} {
			controls[i] = &testVolumeControl{clientID: id, volume: test.before[i]}
			m.targets[id] = &target{clientID: id, volumeControl: controls[i]}
			members = append(members, groupMember{ID: id})
		}
		m.groupVolume(members, test.volume)
		if got := [2]int{controls[0].volume, controls[1].volume}; got != test.after {
			t.Errorf("%s: volumes = %v, want %v", test.name, got, test.after)
		}
	}
}

func TestAirPlayVolumeForwardsToTarget(t *testing.T) {
	control := &testVolumeControl{}
	target := &target{}
	target.setClientID("dot")
	target.setVolumeController(control)
	target.onVolume(0.42)
	if control.clientID != "dot" || control.volume != 42 {
		t.Fatalf("volume command = (%q, %d), want (dot, 42)", control.clientID, control.volume)
	}
}

func TestAirPlayVolumePercent(t *testing.T) {
	for _, test := range []struct {
		volume float64
		want   int
	}{
		{-1, 0}, {0, 0}, {0.424, 42}, {0.425, 43}, {1, 100}, {2, 100}, {math.NaN(), 0},
	} {
		if got := airPlayVolumePercent(test.volume); got != test.want {
			t.Errorf("airPlayVolumePercent(%v) = %d, want %d", test.volume, got, test.want)
		}
	}
}

func TestAirPlayTargetName(t *testing.T) {
	for _, test := range []struct{ suffix, want string }{
		{" (Sendspin)", "Cocina (Sendspin)"},
		{"", "Cocina"},
	} {
		if got := airPlayTargetName("Cocina", test.suffix); got != test.want {
			t.Errorf("suffix %q: target name = %q, want %q", test.suffix, got, test.want)
		}
	}
}

func TestVirtualMACIsStableAndLocallyAdministered(t *testing.T) {
	first := virtualMAC("kitchen._sendspin._tcp.local.")
	if first != virtualMAC("kitchen._sendspin._tcp.local.") {
		t.Fatal("virtual MAC changed for the same player")
	}
	if first[0]&1 != 0 || first[0]&2 == 0 {
		t.Fatalf("virtual MAC is not locally-administered unicast: %02x", first[0])
	}
}

func TestHiddenSpeakerOnlyJoinsForGroups(t *testing.T) {
	m := &Manager{memberGroups: map[string][]*pcm.Group{"grouped": {nil}}}
	for _, test := range []struct {
		s    speaker
		want bool
	}{
		{speaker{ID: "solo"}, true},
		{speaker{ID: "solo", Hidden: true}, false},
		{speaker{ID: "grouped", Hidden: true}, true},
	} {
		if got := m.wanted(test.s); got != test.want {
			t.Errorf("wanted(%+v) = %v, want %v", test.s, got, test.want)
		}
	}
}
