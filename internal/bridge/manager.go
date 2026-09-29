// Package bridge turns each configured Sendspin player into one AirPlay target.
package bridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/discovery"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sendspin"
	"github.com/hkfuertes/goplay2-sendspin/internal/airplay"
	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
	"github.com/hkfuertes/goplay2-sendspin/internal/raop"
)

const (
	defaultPortBase   = 7000
	defaultPortRange  = 10
	defaultServerPort = 8927
	defaultServerName = "AirPlay Sendspin"
)

type Config struct {
	PortBase   uint16
	PortRange  uint16
	ConfigPath string
	ServerPort uint16
	ServerName string
}

type volumeController interface {
	SetVolume(clientID string, volume int) error
	Clients() []sendspin.ClientInfo
}

type Manager struct {
	portRange     uint16
	serverPort    uint16
	serverName    string
	airPlaySuffix string
	registry      *registry

	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	targets       map[string]*target // local speaker ID -> target
	groups        []*groupTarget
	memberGroups  map[string][]*pcm.Group // speaker ID -> its groups; fixed once Run starts
	inbound       *sendspin.Server
	inboundSource *pcm.Pipeline
	inboundDone   chan error
}

func New(config Config) (*Manager, error) {
	if config.PortBase == 0 {
		config.PortBase = defaultPortBase
	}
	if config.PortRange == 0 {
		config.PortRange = defaultPortRange
	}
	if config.PortRange < 3 {
		return nil, fmt.Errorf("AirPlay port range must contain at least three ports")
	}
	if config.ServerPort == 0 {
		config.ServerPort = defaultServerPort
	}
	if config.ServerName == "" {
		config.ServerName = defaultServerName
	}
	registry, err := loadRegistry(config.ConfigPath, config.PortBase, config.PortRange)
	if err != nil {
		return nil, err
	}
	return &Manager{
		portRange:     config.PortRange,
		serverPort:    config.ServerPort,
		serverName:    config.ServerName,
		airPlaySuffix: registry.airPlaySuffix(),
		registry:      registry,
		targets:       make(map[string]*target),
	}, nil
}

func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	defer m.Close()

	if err := m.startGroups(); err != nil {
		return err
	}
	if err := m.startInbound(ctx); err != nil {
		return err
	}
	if err := m.startConfigured(ctx); err != nil {
		return err
	}

	discoveryManager := discovery.NewManager(discovery.Config{})
	if err := discoveryManager.BrowseClients(); err != nil {
		return fmt.Errorf("browse Sendspin players: %w", err)
	}
	defer discoveryManager.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case info := <-discoveryManager.Clients():
			if err := m.add(ctx, info); err != nil {
				log.Printf("Sendspin player target: %v", err)
			}
		}
	}
}

// startInbound advertises one _sendspin-server._tcp endpoint. Clients that
// connect to it are routed by client_id to their own target Pipeline.
func (m *Manager) startInbound(ctx context.Context) error {
	ip, err := airplay.LANIPv4()
	if err != nil {
		return err
	}
	source, err := pcm.NewPipeline(pcm.SendspinSampleRate)
	if err != nil {
		return err
	}
	server, err := sendspin.NewServer(sendspin.ServerConfig{
		Port:            int(m.serverPort),
		Name:            m.serverName,
		Source:          source,
		BitDepth:        16,
		PreferredCodec:  "pcm",
		SourceForClient: m.sourceForInboundClient,
		EnableMDNS:      true,
		MDNSIPs:         []net.IP{ip},
		KeepSourceOpen:  true,
	})
	if err != nil {
		_ = source.Close()
		return err
	}
	done := make(chan error, 1)

	m.mu.Lock()
	m.inbound, m.inboundSource, m.inboundDone = server, source, done
	m.mu.Unlock()
	go func() { done <- server.Start() }()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if server.Addr() != nil {
			return nil
		}
		select {
		case err := <-done:
			m.mu.Lock()
			if m.inbound == server {
				m.inbound, m.inboundSource, m.inboundDone = nil, nil, nil
			}
			m.mu.Unlock()
			_ = source.Close()
			return fmt.Errorf("start inbound Sendspin server: %w", err)
		case <-ctx.Done():
			server.Stop()
			<-done
			m.mu.Lock()
			if m.inbound == server {
				m.inbound, m.inboundSource, m.inboundDone = nil, nil, nil
			}
			m.mu.Unlock()
			_ = source.Close()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// startGroups advertises one AirPlay target per configured group. It runs
// before any speaker target exists, so every speaker is built with its groups.
func (m *Manager) startGroups() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memberGroups = make(map[string][]*pcm.Group)
	for _, g := range m.registry.groups() {
		audio, err := pcm.NewGroup()
		if err != nil {
			return err
		}
		members := g.Speakers
		airPlay, err := advertiseAirPlay("group:"+g.ID, g.AirPlayName, m.airPlaySuffix, g.Port, m.portRange, audio, func(volume float64) { m.groupVolume(members, volume) })
		if err != nil {
			_ = audio.Close()
			return err
		}
		m.groups = append(m.groups, &groupTarget{audio: audio, airPlay: airPlay})
		for _, member := range members {
			m.memberGroups[member.ID] = append(m.memberGroups[member.ID], audio)
		}
		log.Printf("AirPlay group %q (%s) -> %d speakers", g.AirPlayName, g.ID, len(members))
	}
	return nil
}

// groupVolume moves the members' average volume to the group's AirPlay volume
// and keeps their differences, as aiosendspin's group volume does.
func (m *Manager) groupVolume(members []groupMember, volume float64) {
	m.mu.Lock()
	targets := make([]*target, 0, len(members))
	for _, member := range members {
		if t := m.targets[member.ID]; t != nil {
			targets = append(targets, t)
		}
	}
	m.mu.Unlock()
	var players []*target
	var levels []float64
	for _, t := range targets {
		if level, ok := t.playerVolume(); ok {
			players = append(players, t)
			levels = append(levels, float64(level))
		}
	}
	spreadVolume(levels, float64(airPlayVolumePercent(volume)))
	for i, t := range players {
		t.setPlayerVolume(int(math.Round(levels[i])))
	}
}

// spreadVolume shifts every level by the same amount so their mean becomes
// target. What a level loses to the 0..100 clamp is shared among the levels
// that still have room.
func spreadVolume(levels []float64, target float64) {
	if len(levels) == 0 {
		return
	}
	var sum float64
	for _, level := range levels {
		sum += level
	}
	delta := target - sum/float64(len(levels))
	active := make([]int, len(levels))
	for i := range active {
		active[i] = i
	}
	// A pass that clamps nobody has applied all of delta; any other pass drops
	// a level, so this ends within one pass per level.
	for {
		var lost float64
		next := active[:0]
		for _, i := range active {
			level := levels[i] + delta
			switch {
			case level > 100:
				lost += level - 100
				level = 100
			case level < 0:
				lost += level
				level = 0
			default:
				next = append(next, i)
			}
			levels[i] = level
		}
		if len(next) == len(active) || len(next) == 0 {
			return
		}
		delta = lost / float64(len(next))
		active = next
	}
}

type groupTarget struct {
	audio   *pcm.Group
	airPlay *airPlayTarget
}

func (m *Manager) startConfigured(ctx context.Context) error {
	m.mu.Lock()
	speakers := m.registry.speakers()
	m.mu.Unlock()
	for _, speaker := range speakers {
		if speaker.Direction == directionOutbound && !speaker.hasEndpoint() {
			log.Printf("Configured speaker %q has no Sendspin endpoint; waiting for discovery", speaker.ID)
			continue
		}
		if err := m.ensureTarget(ctx, speaker); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	inbound, inboundSource, inboundDone := m.inbound, m.inboundSource, m.inboundDone
	m.inbound, m.inboundSource, m.inboundDone, m.ctx = nil, nil, nil, nil
	targets := make([]*target, 0, len(m.targets))
	for _, t := range m.targets {
		targets = append(targets, t)
	}
	clear(m.targets)
	groups := m.groups
	m.groups = nil
	m.mu.Unlock()

	if inbound != nil {
		inbound.Stop()
		if inboundDone != nil {
			<-inboundDone
		}
	}
	if inboundSource != nil {
		_ = inboundSource.Close()
	}
	for _, t := range targets {
		t.Close()
	}
	for _, g := range groups {
		g.airPlay.Close()
		_ = g.audio.Close()
	}
}

// add persists a newly discovered client before exposing its AirPlay target.
func (m *Manager) add(ctx context.Context, info *discovery.ClientInfo) error {
	if info == nil {
		return fmt.Errorf("invalid Sendspin player discovery")
	}

	m.mu.Lock()
	speaker, added, err := m.registry.upsertOutbound(*info)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if speaker.Direction != directionOutbound {
		return nil // an explicitly inbound target never gets a second session
	}
	if added {
		log.Printf("Recorded discovered Sendspin player %q as %q", info.Name, speaker.ID)
	}
	return m.ensureTarget(ctx, speaker)
}

// wanted reports whether a speaker needs a Sendspin session: a hidden speaker
// outside every group is left free for other Sendspin servers.
func (m *Manager) wanted(s speaker) bool { return !s.Hidden || len(m.memberGroups[s.ID]) > 0 }

func (m *Manager) ensureTarget(ctx context.Context, speaker speaker) error {
	m.mu.Lock()
	if !m.wanted(speaker) {
		m.mu.Unlock()
		return nil
	}
	if t := m.targets[speaker.ID]; t != nil {
		t.updateEndpoint(speaker.Endpoint)
		m.mu.Unlock()
		return nil
	}
	t, err := newTarget(ctx, speaker, m.portRange, m.airPlaySuffix, m.memberGroups[speaker.ID], m.rememberClient)
	if err == nil {
		m.targets[speaker.ID] = t
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	go t.run()
	if speaker.Direction == directionOutbound {
		log.Printf("AirPlay target %q (%s) -> Sendspin %s:%d", speaker.AirPlayName, speaker.ID, speaker.Endpoint.Host, speaker.Endpoint.Port)
	} else {
		log.Printf("AirPlay target %q (%s) waiting for inbound Sendspin", speaker.AirPlayName, speaker.ID)
	}
	return nil
}

// rememberClient binds a player to its speaker and returns the configured
// delay_ms, materializing zero in config.xml on its first hello.
func (m *Manager) rememberClient(speakerID, clientID string) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.registry.setClientID(speakerID, clientID); err != nil {
		return 0, err
	}
	ms, err := m.registry.delay(speakerID)
	return time.Duration(ms) * time.Millisecond, err
}

func (m *Manager) sourceForInboundClient(hello protocol.ClientHello) (sendspin.AudioSource, error) {
	m.mu.Lock()
	speaker, _, err := m.registry.upsertInbound(hello.ClientID, hello.Name)
	if err == nil && !m.wanted(speaker) {
		err = fmt.Errorf("speaker %q is hidden and in no group", speaker.ID)
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if t := m.targets[speaker.ID]; t != nil {
		control := m.inbound
		m.mu.Unlock()
		if err := t.configurePlayer(hello.PlayerV1Support); err != nil {
			return nil, err
		}
		if err := t.onClientHello(hello); err != nil {
			return nil, err
		}
		t.setVolumeController(control)
		return t.pipeline, nil
	}
	if m.ctx == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("bridge is not running")
	}
	t, err := newTarget(m.ctx, speaker, m.portRange, m.airPlaySuffix, m.memberGroups[speaker.ID], m.rememberClient)
	if err == nil {
		m.targets[speaker.ID] = t
	}
	control := m.inbound
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	go t.run()
	log.Printf("AirPlay target %q (%s) claimed by inbound Sendspin", speaker.AirPlayName, speaker.ID)
	if err := t.configurePlayer(hello.PlayerV1Support); err != nil {
		return nil, err
	}
	if err := t.onClientHello(hello); err != nil {
		return nil, err
	}
	t.setVolumeController(control)
	return t.pipeline, nil
}

type target struct {
	speaker        speaker
	pipeline       *pcm.Mix
	airPlay        *airPlayTarget
	rememberClient func(speakerID, clientID string) (time.Duration, error)
	sessionMu      sync.RWMutex
	volumeControl  volumeController
	clientID       string
	infoMu         sync.RWMutex
	info           discovery.ClientInfo
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	once           sync.Once
}

func newTarget(parent context.Context, speaker speaker, portRange uint16, suffix string, groups []*pcm.Group, rememberClient func(string, string) (time.Duration, error)) (*target, error) {
	if speaker.ID == "" || speaker.AirPlayName == "" || speaker.Port == 0 {
		return nil, fmt.Errorf("invalid configured speaker")
	}
	ctx, cancel := context.WithCancel(parent)
	pipeline, err := pcm.NewMix(pcm.SendspinSampleRate, groups) // retain at least one second
	if err != nil {
		cancel()
		return nil, err
	}
	t := &target{
		speaker:        speaker,
		pipeline:       pipeline,
		rememberClient: rememberClient,
		clientID:       speaker.ClientID,
		info:           speaker.clientInfo(),
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
	}

	if speaker.Hidden {
		return t, nil
	}
	if t.airPlay, err = advertiseAirPlay(speaker.ID, speaker.AirPlayName, suffix, speaker.Port, portRange, pipeline, t.onVolume); err != nil {
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	return t, nil
}

// airPlayTarget is the advertised libraop receiver of a speaker or group.
type airPlayTarget struct {
	receiver   *raop.Receiver
	advertiser *airplay.Advertiser
}

// advertiseAirPlay publishes name+suffix with an AirPlay identity derived
// from key and feeds its PCM to sink.
func advertiseAirPlay(key, name, suffix string, port, portRange uint16, sink pcm.Sink, onVolume func(float64)) (*airPlayTarget, error) {
	ip, err := airplay.LANIPv4()
	if err != nil {
		return nil, err
	}
	mac := virtualMAC(key)
	name = airPlayTargetName(name, suffix)
	receiver, err := raop.New(raop.Config{
		Name:      name,
		MAC:       mac,
		Host:      ip,
		PortBase:  port,
		PortRange: portRange,
		Source:    sink,
		OnVolume:  onVolume,
	})
	if err != nil {
		return nil, err
	}
	advertiser, err := airplay.Advertise(name, mac, ip, receiver.Port())
	if err != nil {
		receiver.Close()
		return nil, err
	}
	return &airPlayTarget{receiver: receiver, advertiser: advertiser}, nil
}

func (a *airPlayTarget) Close() {
	if a == nil {
		return // hidden speaker
	}
	a.advertiser.Close()
	a.receiver.Close()
}

func (t *target) run() {
	defer close(t.done)
	if t.speaker.Direction == directionInbound {
		<-t.ctx.Done()
		return
	}

	for {
		info := t.clientInfo()
		if !validClientInfo(info) {
			log.Printf("Sendspin target %q has no endpoint; retrying", t.speaker.ID)
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		server, err := sendspin.NewServer(sendspin.ServerConfig{
			Name:           t.speaker.AirPlayName,
			Source:         t.pipeline,
			BitDepth:       16, // initial native-AirPlay profile; callback may select 24-bit
			PreferredCodec: "pcm",
			OnClientHello:  t.onClientHello,
			OnPlayerHello:  t.configurePlayer,
			EnableMDNS:     false,
			KeepSourceOpen: true,
		})
		if err == nil {
			t.setVolumeController(server)
			err = server.StartOutbound(t.ctx, &info)
			t.setVolumeController(nil)
		}
		if t.ctx.Err() != nil {
			return
		}
		log.Printf("Sendspin session for %q ended: %v; retrying", t.speaker.ID, err)
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (t *target) onClientHello(hello protocol.ClientHello) error {
	var delay time.Duration
	if t.rememberClient != nil {
		var err error
		if delay, err = t.rememberClient(t.speaker.ID, hello.ClientID); err != nil {
			return err
		}
	}
	t.setClientID(hello.ClientID)
	if delay > 0 && t.pipeline.Grouped() {
		log.Printf("Sendspin player %q: group audio held back %v (delay_ms)", t.speaker.ID, delay)
	}
	t.pipeline.SetDelay(delay)
	return nil
}

func (t *target) updateEndpoint(endpoint endpoint) {
	if t.speaker.Direction != directionOutbound {
		return
	}
	t.infoMu.Lock()
	t.info = endpoint.clientInfo(t.speaker.AirPlayName)
	t.infoMu.Unlock()
}

func (t *target) clientInfo() discovery.ClientInfo {
	t.infoMu.RLock()
	defer t.infoMu.RUnlock()
	return t.info
}

func (t *target) Close() {
	t.once.Do(func() {
		t.cancel()
		<-t.done
		t.airPlay.Close()
		_ = t.pipeline.Close()
	})
}

func (t *target) configurePlayer(support *protocol.PlayerV1Support) error {
	var prefer pcm.Format
	if t.pipeline.Grouped() {
		prefer = pcm.GroupFormat
	}
	format, err := selectFormat(support, prefer)
	if err != nil {
		return err
	}
	if err := t.pipeline.Configure(format); err != nil {
		return err
	}
	log.Printf("Sendspin player %q selected %dHz/%d-bit PCM", t.speaker.ID, format.SampleRate, format.BitDepth)
	if prefer != (pcm.Format{}) && format != prefer {
		log.Printf("Sendspin player %q cannot take %dHz/%d-bit; it will not play its groups", t.speaker.ID, prefer.SampleRate, prefer.BitDepth)
	}
	return nil
}

func (s speaker) hasEndpoint() bool {
	return s.Endpoint.Host != "" && s.Endpoint.Port > 0
}

func (s speaker) clientInfo() discovery.ClientInfo {
	return s.Endpoint.clientInfo(s.AirPlayName)
}

func (e endpoint) clientInfo(name string) discovery.ClientInfo {
	return discovery.ClientInfo{
		Instance: e.Instance,
		Name:     name,
		Host:     e.Host,
		Port:     e.Port,
		Path:     normalizePath(e.Path),
	}
}

func validClientInfo(info discovery.ClientInfo) bool {
	return info.Name != "" && info.Host != "" && info.Port > 0
}

func (t *target) setVolumeController(control volumeController) {
	t.sessionMu.Lock()
	t.volumeControl = control
	t.sessionMu.Unlock()
}

func (t *target) setClientID(clientID string) {
	t.sessionMu.Lock()
	t.clientID = clientID
	t.sessionMu.Unlock()
}

func (t *target) session() (volumeController, string) {
	t.sessionMu.RLock()
	defer t.sessionMu.RUnlock()
	return t.volumeControl, t.clientID
}

func (t *target) onVolume(volume float64) { t.setPlayerVolume(airPlayVolumePercent(volume)) }

func (t *target) setPlayerVolume(volume int) {
	if control, clientID := t.session(); control != nil && clientID != "" {
		_ = control.SetVolume(clientID, volume)
	}
}

// playerVolume returns the volume the connected player last reported.
func (t *target) playerVolume() (int, bool) {
	control, clientID := t.session()
	if control == nil || clientID == "" {
		return 0, false
	}
	for _, c := range control.Clients() {
		if c.ID == clientID {
			return c.Volume, true
		}
	}
	return 0, false
}

func airPlayVolumePercent(volume float64) int {
	if math.IsNaN(volume) || volume <= 0 {
		return 0
	}
	if volume >= 1 {
		return 100
	}
	return int(math.Round(volume * 100))
}

func airPlayTargetName(name, suffix string) string { return name + suffix }

func virtualMAC(key string) [6]byte {
	sum := sha256.Sum256([]byte(key))
	mac := [6]byte(sum[:6])
	mac[0] = mac[0]&^1 | 2 // locally administered, unicast
	return mac
}
