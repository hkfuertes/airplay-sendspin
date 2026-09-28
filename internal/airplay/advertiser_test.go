package airplay

import (
	"net"
	"testing"
)

func TestServiceNameUsesStableRAOPPrefix(t *testing.T) {
	mac := [6]byte{2, 1, 2, 3, 4, 5}
	got := serviceName("Kitchen", mac)
	if got != "020102030405@Kitchen" {
		t.Fatalf("serviceName() = %q", got)
	}
	if got := hostName(mac); got != "raop-020102030405.local." {
		t.Fatalf("hostName() = %q", got)
	}
}

func TestAdvertiseRejectsNonIPv4(t *testing.T) {
	if _, err := Advertise("Kitchen", [6]byte{2, 1, 2, 3, 4, 5}, net.IPv6loopback, 7000); err == nil {
		t.Fatal("Advertise accepted a non-IPv4 address")
	}
}
