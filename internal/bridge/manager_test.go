package bridge

import (
	"math"
	"testing"
)

type testVolumeControl struct {
	clientID string
	volume   int
}

func (c *testVolumeControl) SetVolume(clientID string, volume int) error {
	c.clientID, c.volume = clientID, volume
	return nil
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
	if got := airPlayTargetName("Cocina"); got != "Cocina (Sendspin)" {
		t.Fatalf("target name = %q", got)
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
