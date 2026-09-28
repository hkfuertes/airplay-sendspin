package bridge

import "testing"

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
