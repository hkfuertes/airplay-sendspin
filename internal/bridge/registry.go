package bridge

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Sendspin/sendspin-go/pkg/discovery"
)

const (
	defaultConfigPath = "config.xml"
	directionInbound  = "inbound"
	directionOutbound = "outbound"
)

type registryDocument struct {
	XMLName  xml.Name  `xml:"airplay-sendspin"`
	Version  string    `xml:"version,attr"`
	Speakers []speaker `xml:"speakers>speaker"`
	Groups   []group   `xml:"groups>group"`
}

// group is one extra AirPlay target that plays in sync on every member
// speaker. Groups are edited by hand; members are stable speaker IDs.
type group struct {
	ID          string        `xml:"id,attr"`
	AirPlayName string        `xml:"airplay_name,attr"`
	Port        uint16        `xml:"port,attr"`
	Speakers    []groupMember `xml:"speaker"`
}

type groupMember struct {
	ID string `xml:"id,attr"`
}

// speaker is one independently advertised AirPlay target. ID is local and
// stable for config references; ClientID is the Sendspin identity used to
// claim an inbound player connection.
type speaker struct {
	ID          string   `xml:"id,attr"`
	ClientID    string   `xml:"client_id,attr,omitempty"`
	AirPlayName string   `xml:"airplay_name,attr"`
	Direction   string   `xml:"direction,attr"`
	Port        uint16   `xml:"port,attr"`
	Endpoint    endpoint `xml:"endpoint"`
}

type endpoint struct {
	Instance string `xml:"instance,attr,omitempty"`
	Host     string `xml:"host,attr,omitempty"`
	Port     int    `xml:"port,attr,omitempty"`
	Path     string `xml:"path,attr,omitempty"`
}

type registry struct {
	path      string
	portBase  uint16
	portRange uint16
	doc       registryDocument
}

func loadRegistry(path string, portBase, portRange uint16) (*registry, error) {
	if path == "" {
		path = defaultConfigPath
	}
	r := &registry{
		path:      path,
		portBase:  portBase,
		portRange: portRange,
		doc:       registryDocument{Version: "1"},
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := xml.Unmarshal(data, &r.doc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if r.doc.XMLName.Local != "airplay-sendspin" {
		return nil, fmt.Errorf("config %s has root <%s>, want <airplay-sendspin>", path, r.doc.XMLName.Local)
	}
	if r.doc.Version == "" {
		r.doc.Version = "1"
	}

	dirty := false
	seenIDs := make(map[string]bool, len(r.doc.Speakers))
	seenClientIDs := make(map[string]bool, len(r.doc.Speakers))
	for i := range r.doc.Speakers {
		s := &r.doc.Speakers[i]
		if s.ID == "" || seenIDs[s.ID] {
			return nil, fmt.Errorf("config %s has missing or duplicate speaker id", path)
		}
		seenIDs[s.ID] = true
		if s.ClientID != "" {
			if seenClientIDs[s.ClientID] {
				return nil, fmt.Errorf("config %s has duplicate Sendspin client_id %q", path, s.ClientID)
			}
			seenClientIDs[s.ClientID] = true
		}
		if s.Direction == "" {
			s.Direction = directionOutbound
			dirty = true
		}
		if s.Direction != directionInbound && s.Direction != directionOutbound {
			return nil, fmt.Errorf("speaker %q has invalid direction %q", s.ID, s.Direction)
		}
		if s.AirPlayName == "" {
			s.AirPlayName = s.ID
			dirty = true
		}
		if s.Port == 0 {
			port, err := r.nextPort(r.doc)
			if err != nil {
				return nil, err
			}
			s.Port = port
			dirty = true
		}
		s.Endpoint.Path = normalizePath(s.Endpoint.Path)
	}
	seenGroups := make(map[string]bool, len(r.doc.Groups))
	for i := range r.doc.Groups {
		g := &r.doc.Groups[i]
		if g.ID == "" || seenGroups[g.ID] {
			return nil, fmt.Errorf("config %s has missing or duplicate group id", path)
		}
		seenGroups[g.ID] = true
		members := make(map[string]bool, len(g.Speakers))
		for _, member := range g.Speakers {
			if !seenIDs[member.ID] || members[member.ID] {
				return nil, fmt.Errorf("group %q has unknown or duplicate speaker %q", g.ID, member.ID)
			}
			members[member.ID] = true
		}
		if g.AirPlayName == "" {
			g.AirPlayName = g.ID
			dirty = true
		}
		if g.Port == 0 {
			port, err := r.nextPort(r.doc)
			if err != nil {
				return nil, err
			}
			g.Port = port
			dirty = true
		}
	}
	if dirty {
		if err := r.save(r.doc); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *registry) speakers() []speaker {
	out := make([]speaker, len(r.doc.Speakers))
	copy(out, r.doc.Speakers)
	return out
}

func (r *registry) groups() []group { return r.cloneDocument().Groups }

func (r *registry) speaker(id string) (speaker, bool) {
	for _, s := range r.doc.Speakers {
		if s.ID == id {
			return s, true
		}
	}
	return speaker{}, false
}

// upsertOutbound records the mDNS endpoint before it is made visible as an
// AirPlay target. Repeated discovery packets do not rewrite an unchanged file.
func (r *registry) upsertOutbound(info discovery.ClientInfo) (speaker, bool, error) {
	if info.Name == "" || info.Host == "" || info.Port < 1 {
		return speaker{}, false, fmt.Errorf("invalid Sendspin player discovery")
	}
	next := r.cloneDocument()
	for i := range next.Speakers {
		s := &next.Speakers[i]
		if !sameEndpoint(s.Endpoint, info) {
			continue
		}
		if s.Direction != directionOutbound {
			return *s, false, nil
		}
		updated := endpointFrom(info)
		if s.Endpoint == updated && s.AirPlayName != "" {
			return *s, false, nil
		}
		s.Endpoint = updated
		if s.AirPlayName == "" {
			s.AirPlayName = info.Name
		}
		if err := r.save(next); err != nil {
			return speaker{}, false, err
		}
		return *s, false, nil
	}

	port, err := r.nextPort(next)
	if err != nil {
		return speaker{}, false, err
	}
	s := speaker{
		ID:          r.nextID(next.Speakers, info.Name),
		AirPlayName: info.Name,
		Direction:   directionOutbound,
		Port:        port,
		Endpoint:    endpointFrom(info),
	}
	next.Speakers = append(next.Speakers, s)
	if err := r.save(next); err != nil {
		return speaker{}, false, err
	}
	return s, true, nil
}

func (r *registry) setClientID(id, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("Sendspin client_id is required")
	}
	next := r.cloneDocument()
	for _, s := range next.Speakers {
		if s.ClientID == clientID && s.ID != id {
			return fmt.Errorf("Sendspin client_id %q already belongs to %s", clientID, s.ID)
		}
	}
	for i := range next.Speakers {
		if next.Speakers[i].ID != id {
			continue
		}
		if next.Speakers[i].ClientID == clientID {
			return nil
		}
		next.Speakers[i].ClientID = clientID
		return r.save(next)
	}
	return fmt.Errorf("unknown speaker %q", id)
}

// upsertInbound claims a player that connected to the bridge's advertised
// Sendspin server. An existing outbound record is deliberately not reused:
// one speaker gets exactly one connection direction.
func (r *registry) upsertInbound(clientID, name string) (speaker, bool, error) {
	if clientID == "" {
		return speaker{}, false, fmt.Errorf("Sendspin client_id is required")
	}
	for _, s := range r.doc.Speakers {
		if s.ClientID != clientID {
			continue
		}
		if s.Direction != directionInbound {
			return speaker{}, false, fmt.Errorf("speaker %q is configured for outbound Sendspin", s.ID)
		}
		return s, false, nil
	}

	next := r.cloneDocument()
	port, err := r.nextPort(next)
	if err != nil {
		return speaker{}, false, err
	}
	if name == "" {
		name = clientID
	}
	s := speaker{
		ID:          r.nextID(next.Speakers, name),
		ClientID:    clientID,
		AirPlayName: name,
		Direction:   directionInbound,
		Port:        port,
	}
	next.Speakers = append(next.Speakers, s)
	if err := r.save(next); err != nil {
		return speaker{}, false, err
	}
	return s, true, nil
}

func (r *registry) cloneDocument() registryDocument {
	next := r.doc
	next.Speakers = make([]speaker, len(r.doc.Speakers))
	copy(next.Speakers, r.doc.Speakers)
	next.Groups = make([]group, len(r.doc.Groups))
	copy(next.Groups, r.doc.Groups)
	for i := range next.Groups {
		next.Groups[i].Speakers = append([]groupMember(nil), r.doc.Groups[i].Speakers...)
	}
	return next
}

func (r *registry) nextID(speakers []speaker, name string) string {
	used := make(map[string]bool, len(speakers))
	for _, s := range speakers {
		used[s.ID] = true
	}
	base := speakerID(name)
	if base == "" {
		base = "speaker"
	}
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if !used[id] {
			return id
		}
	}
}

func speakerID(name string) string {
	var out strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func (r *registry) nextPort(doc registryDocument) (uint16, error) {
	used := make(map[uint16]bool, len(doc.Speakers)+len(doc.Groups))
	for _, s := range doc.Speakers {
		used[s.Port] = true
	}
	for _, g := range doc.Groups {
		used[g.Port] = true
	}
	for port := int(r.portBase); port+int(r.portRange)-1 <= 65535; port += int(r.portRange) {
		if !used[uint16(port)] {
			return uint16(port), nil
		}
	}
	return 0, fmt.Errorf("no AirPlay port range left")
}

func (r *registry) save(doc registryDocument) error {
	doc.Version = "1"
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config %s: %w", r.path, err)
	}
	data = append([]byte(xml.Header), append(data, '\n')...)

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config.xml-*")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", r.path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set config mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync config %s: %w", r.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config %s: %w", r.path, err)
	}
	if err := os.Rename(name, r.path); err != nil {
		return fmt.Errorf("replace config %s: %w", r.path, err)
	}
	r.doc = doc
	return nil
}

func endpointFrom(info discovery.ClientInfo) endpoint {
	return endpoint{
		Instance: info.Instance,
		Host:     info.Host,
		Port:     info.Port,
		Path:     normalizePath(info.Path),
	}
}

func sameEndpoint(endpoint endpoint, info discovery.ClientInfo) bool {
	if endpoint.Instance != "" && info.Instance != "" {
		return endpoint.Instance == info.Instance
	}
	return endpoint.Host == info.Host && endpoint.Port == info.Port && normalizePath(endpoint.Path) == normalizePath(info.Path)
}

func normalizePath(path string) string {
	if path == "" {
		return "/sendspin"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
