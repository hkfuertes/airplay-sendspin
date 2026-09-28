// Package bridge turns each configured Sendspin player into one AirPlay target.
package bridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
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
	airPlaySuffix     = " (Sendspin)"
)

type Config struct {
	PortBase   uint16
	PortRange  uint16
	ConfigPath string
	ServerPort uint16
	ServerName string
}

type Manager struct {
	portRange  uint16
	serverPort uint16
	serverName string
	registry   *registry

	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	targets       map[string]*target // local speaker ID -> target
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
		portRange:  config.PortRange,
		serverPort: config.ServerPort,
		serverName: config.ServerName,
		registry:   registry,
		targets:    make(map[string]*target),
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

func (m *Manager) ensureTarget(ctx context.Context, speaker speaker) error {
	m.mu.Lock()
	if t := m.targets[speaker.ID]; t != nil {
		t.updateEndpoint(speaker.Endpoint)
		m.mu.Unlock()
		return nil
	}
	t, err := newTarget(ctx, speaker, m.portRange, m.rememberClient)
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

func (m *Manager) rememberClient(speakerID, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registry.setClientID(speakerID, clientID)
}

func (m *Manager) sourceForInboundClient(hello protocol.ClientHello) (sendspin.AudioSource, error) {
	m.mu.Lock()
	speaker, _, err := m.registry.upsertInbound(hello.ClientID, hello.Name)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if t := m.targets[speaker.ID]; t != nil {
		m.mu.Unlock()
		if err := t.configurePlayer(hello.PlayerV1Support); err != nil {
			return nil, err
		}
		return t.pipeline, nil
	}
	if m.ctx == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("bridge is not running")
	}
	t, err := newTarget(m.ctx, speaker, m.portRange, m.rememberClient)
	if err == nil {
		m.targets[speaker.ID] = t
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	go t.run()
	log.Printf("AirPlay target %q (%s) claimed by inbound Sendspin", speaker.AirPlayName, speaker.ID)
	if err := t.configurePlayer(hello.PlayerV1Support); err != nil {
		return nil, err
	}
	return t.pipeline, nil
}

type target struct {
	speaker        speaker
	pipeline       *pcm.Pipeline
	receiver       *raop.Receiver
	advertiser     *airplay.Advertiser
	rememberClient func(speakerID, clientID string) error
	infoMu         sync.RWMutex
	info           discovery.ClientInfo
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	once           sync.Once
}

func newTarget(parent context.Context, speaker speaker, portRange uint16, rememberClient func(string, string) error) (*target, error) {
	if speaker.ID == "" || speaker.AirPlayName == "" || speaker.Port == 0 {
		return nil, fmt.Errorf("invalid configured speaker")
	}
	ctx, cancel := context.WithCancel(parent)
	pipeline, err := pcm.NewPipeline(pcm.SendspinSampleRate) // retain at least one second
	if err != nil {
		cancel()
		return nil, err
	}
	t := &target{
		speaker:        speaker,
		pipeline:       pipeline,
		rememberClient: rememberClient,
		info:           speaker.clientInfo(),
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
	}

	ip, err := airplay.LANIPv4()
	if err != nil {
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	mac := virtualMAC(speaker.ID)
	airPlayName := airPlayTargetName(speaker.AirPlayName)
	receiver, err := raop.New(raop.Config{
		Name:      airPlayName,
		MAC:       mac,
		Host:      ip,
		PortBase:  speaker.Port,
		PortRange: portRange,
		Source:    t.pipeline,
	})
	if err != nil {
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	t.receiver = receiver

	advertiser, err := airplay.Advertise(airPlayName, mac, ip, receiver.Port())
	if err != nil {
		receiver.Close()
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	t.advertiser = advertiser
	return t, nil
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
			err = server.StartOutbound(t.ctx, &info)
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
	if t.rememberClient == nil {
		return nil
	}
	return t.rememberClient(t.speaker.ID, hello.ClientID)
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
		t.advertiser.Close()
		t.receiver.Close()
		_ = t.pipeline.Close()
	})
}

func (t *target) configurePlayer(support *protocol.PlayerV1Support) error {
	format, err := selectFormat(support)
	if err != nil {
		return err
	}
	if err := t.pipeline.Configure(format); err != nil {
		return err
	}
	log.Printf("Sendspin player %q selected %dHz/%d-bit PCM", t.speaker.ID, format.SampleRate, format.BitDepth)
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

func airPlayTargetName(name string) string { return name + airPlaySuffix }

func virtualMAC(key string) [6]byte {
	sum := sha256.Sum256([]byte(key))
	mac := [6]byte(sum[:6])
	mac[0] = mac[0]&^1 | 2 // locally administered, unicast
	return mac
}
