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
	if !strings.Contains(string(data), `client_id="echo-kitchen"`) || !strings.Contains(string(data), `path="/sendspin"`) || !strings.Contains(string(data), `airplay_suffix=" (Sendspin)"`) {
		t.Fatalf("config.xml missing persisted identity, endpoint, or suffix:\n%s", data)
	}
}

func TestRegistryNormalizesOlderConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	data := `<?xml version="1.0"?><airplay-sendspin version="1" airplay_suffix=""><speakers><speaker id="dot" direction="outbound" port="7000"/></speakers></airplay-sendspin>`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.airPlaySuffix(); got != "" {
		t.Fatalf("suffix = %q, want empty", got)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `airplay_suffix=""`) || !strings.Contains(string(saved), `hidden="false"`) {
		t.Fatalf("config.xml not normalized:\n%s", saved)
	}
}

func TestRegistryDelayPersistsAndHonorsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	r, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := r.upsertOutbound(discovery.ClientInfo{Name: "Dot", Host: "192.0.2.10", Port: 8928})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.delay(s.ID); err != nil || got != 0 {
		t.Fatalf("initial delay = (%d, %v), want (0, nil)", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `delay_ms="0"`) {
		t.Fatalf("config.xml missing initial zero delay:\n%s", data)
	}

	// A hand-written value is authoritative after discovery.
	data = []byte(strings.Replace(string(data), `delay_ms="0"`, `delay_ms="100"`, 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.delay(s.ID); err != nil || got != 100 {
		t.Fatalf("configured delay = (%d, %v), want (100, nil)", got, err)
	}
}

func TestRegistryRejectsInvalidDelay(t *testing.T) {
	for _, delay := range []string{"-1", "501"} {
		t.Run(delay, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.xml")
			data := `<?xml version="1.0"?><airplay-sendspin version="1"><speakers><speaker id="dot" direction="outbound" port="7000" delay_ms="` + delay + `"/></speakers></airplay-sendspin>`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRegistry(path, 7000, 10); err == nil {
				t.Fatalf("loaded delay_ms=%s", delay)
			}
		})
	}
}

func TestRegistryGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	config := func(members string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`<?xml version="1.0"?>
<airplay-sendspin version="1"><speakers><speaker id="cocina" direction="inbound" port="7000"/></speakers><groups><group id="whole-house" airplay_name="Toda la casa">`+members+`</group></groups></airplay-sendspin>`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{`<speaker id="salon"/>`, `<speaker id="cocina"/><speaker id="cocina"/>`} {
		config(bad)
		if _, err := loadRegistry(path, 7000, 10); err == nil {
			t.Fatalf("loaded group with members %s", bad)
		}
	}

	config(`<speaker id="cocina"/>`)
	r, err := loadRegistry(path, 7000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if groups := r.groups(); len(groups) != 1 || groups[0].Port != 7010 {
		t.Fatalf("groups = %+v, want whole-house on the next free port", groups)
	}
	speaker, _, err := r.upsertOutbound(discovery.ClientInfo{Name: "Salon", Host: "192.0.2.10", Port: 8928})
	if err != nil || speaker.Port != 7020 {
		t.Fatalf("discovered speaker = (%+v, %v), want port 7020", speaker, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `group id="whole-house"`) || !strings.Contains(string(data), `port="7010"`) {
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
