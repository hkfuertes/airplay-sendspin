// Package bridge turns each discovered Sendspin player into one AirPlay target.
package bridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
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
	defaultPortBase  = 7000
	defaultPortRange = 10
)

type Config struct {
	PortBase  uint16
	PortRange uint16
}

type Manager struct {
	portBase  uint16
	portRange uint16

	mu      sync.Mutex
	cancel  context.CancelFunc
	targets map[string]*target
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
	return &Manager{
		portBase:  config.PortBase,
		portRange: config.PortRange,
		targets:   make(map[string]*target),
	}, nil
}

func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, m.cancel = context.WithCancel(ctx)
	defer m.Close()

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

func (m *Manager) Close() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	targets := make([]*target, 0, len(m.targets))
	for _, t := range m.targets {
		targets = append(targets, t)
	}
	clear(m.targets)
	m.mu.Unlock()

	for _, t := range targets {
		t.Close()
	}
}

func (m *Manager) add(ctx context.Context, info *discovery.ClientInfo) error {
	if info == nil || info.Name == "" {
		return fmt.Errorf("invalid Sendspin player discovery")
	}
	key := info.Instance
	if key == "" {
		key = fmt.Sprintf("%s:%d%s", info.Host, info.Port, info.Path)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.targets[key]; exists {
		return nil
	}

	portBase := int(m.portBase) + len(m.targets)*int(m.portRange)
	if portBase+int(m.portRange)-1 > 65535 {
		return fmt.Errorf("no AirPlay port range left for %s", info.Name)
	}

	t, err := newTarget(ctx, *info, virtualMAC(key), uint16(portBase), m.portRange)
	if err != nil {
		return err
	}
	m.targets[key] = t
	go t.run()
	log.Printf("AirPlay target %q -> Sendspin %s:%d", info.Name, info.Host, info.Port)
	return nil
}

type target struct {
	info       discovery.ClientInfo
	pipeline   *pcm.Pipeline
	receiver   *raop.Receiver
	advertiser *airplay.Advertiser
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	once       sync.Once
}

func newTarget(parent context.Context, info discovery.ClientInfo, mac [6]byte, portBase, portRange uint16) (*target, error) {
	ctx, cancel := context.WithCancel(parent)
	pipeline, err := pcm.NewPipeline(pcm.SendspinSampleRate) // retain at least one second
	if err != nil {
		cancel()
		return nil, err
	}
	t := &target{
		info:     info,
		pipeline: pipeline,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	ip, err := airplay.LANIPv4()
	if err != nil {
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	receiver, err := raop.New(raop.Config{
		Name:      info.Name,
		MAC:       mac,
		Host:      ip,
		PortBase:  portBase,
		PortRange: portRange,
		Source:    t.pipeline,
	})
	if err != nil {
		_ = pipeline.Close()
		cancel()
		return nil, err
	}
	t.receiver = receiver

	advertiser, err := airplay.Advertise(info.Name, mac, ip, receiver.Port())
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
	for {
		server, err := sendspin.NewServer(sendspin.ServerConfig{
			Name:           t.info.Name,
			Source:         t.pipeline,
			BitDepth:       16, // initial native-AirPlay profile; callback may select 24-bit
			PreferredCodec: "pcm",
			OnPlayerHello:  t.configurePlayer,
			EnableMDNS:     false,
			KeepSourceOpen: true,
		})
		if err == nil {
			err = server.StartOutbound(t.ctx, &t.info)
		}
		if t.ctx.Err() != nil {
			return
		}
		log.Printf("Sendspin session for %q ended: %v; retrying", t.info.Name, err)
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
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
	log.Printf("Sendspin player %q selected %dHz/%d-bit PCM", t.info.Name, format.SampleRate, format.BitDepth)
	return nil
}

func virtualMAC(key string) [6]byte {
	sum := sha256.Sum256([]byte(key))
	mac := [6]byte(sum[:6])
	mac[0] = mac[0]&^1 | 2 // locally administered, unicast
	return mac
}
