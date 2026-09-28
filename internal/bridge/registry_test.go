package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/discovery"
)

func TestRegistryPersistsDiscoveredSpeaker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	r, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}

	first, added, err := r.upsertOutbound(discovery.ClientInfo{
		Instance: "kitchen._sendspin._tcp.local.", Name: "Cocina", Host: "192.0.2.10", Port: 8928, Path: "sendspin",
	})
	if err != nil || !added {
		t.Fatalf("upsertOutbound() = (%+v, %v, %v)", first, added, err)
	}
	if first.ID != "cocina" || first.Port != 7000 || first.Direction != directionOutbound {
		t.Fatalf("first speaker = %+v", first)
	}
	if err := r.setClientID(first.ID, "echo-kitchen"); err != nil {
		t.Fatal(err)
	}

	updated, added, err := r.upsertOutbound(discovery.ClientInfo{
		Instance: "kitchen._sendspin._tcp.local.", Name: "Cocina", Host: "192.0.2.99", Port: 8928, Path: "/sendspin",
	})
	if err != nil || added {
		t.Fatalf("updated discovery = (%+v, %v, %v)", updated, added, err)
	}
	if updated.ID != first.ID || updated.Port != first.Port || updated.ClientID != "echo-kitchen" || updated.Endpoint.Host != "192.0.2.99" {
		t.Fatalf("updated speaker = %+v", updated)
	}
	second, added, err := r.upsertOutbound(discovery.ClientInfo{Name: "Cocina", Host: "192.0.2.11", Port: 8928})
	if err != nil || !added || second.ID != "cocina-2" || second.Port != 7010 {
		t.Fatalf("second speaker = (%+v, %v, %v)", second, added, err)
	}

	reloaded, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.speaker(first.ID)
	if !ok || got != updated {
		t.Fatalf("reloaded speaker = %+v, exists=%v; want %+v", got, ok, updated)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `client_id="echo-kitchen"`) || !strings.Contains(string(data), `path="/sendspin"`) {
		t.Fatalf("config.xml missing persisted identity or endpoint:\n%s", data)
	}
}

func TestRegistryPreservesInactiveGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0"?>
<airplay-sendspin version="1"><speakers></speakers><groups><group id="whole-house" airplay_name="Toda la casa"><speaker id="cocina"/></group></groups></airplay-sendspin>`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.upsertOutbound(discovery.ClientInfo{Name: "Cocina", Host: "192.0.2.10", Port: 8928}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `group id="whole-house"`) || !strings.Contains(string(data), `speaker id="cocina"`) {
		t.Fatalf("discovery erased group:\n%s", data)
	}
}

func TestRegistrySeparatesConnectionDirections(t *testing.T) {
	r, err := loadRegistry(filepath.Join(t.TempDir(), "config.xml"), 7000, 10)
	if err != nil {
		t.Fatal(err)
	}

	inbound, added, err := r.upsertInbound("dot-1", "Dot")
	if err != nil || !added || inbound.ID != "dot" || inbound.Direction != directionInbound || inbound.Port != 7000 {
		t.Fatalf("upsertInbound() = (%+v, %v, %v)", inbound, added, err)
	}
	if same, added, err := r.upsertInbound("dot-1", "Renamed"); err != nil || added || same != inbound {
		t.Fatalf("repeat inbound = (%+v, %v, %v)", same, added, err)
	}

	outbound, _, err := r.upsertOutbound(discovery.ClientInfo{Name: "Office", Host: "192.0.2.2", Port: 8928})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.setClientID(outbound.ID, "office-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.upsertInbound("office-1", "Office"); err == nil {
		t.Fatal("inbound claim unexpectedly replaced outbound speaker")
	}
}
