// Package airplay publishes the Go-owned mDNS half of a libraop receiver.
package airplay

import (
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/mdns"
)

type Advertiser struct {
	server *mdns.Server
}

func Advertise(name string, mac [6]byte, ip net.IP, port uint16) (*Advertiser, error) {
	ip = ip.To4()
	if name == "" || ip == nil || port == 0 {
		return nil, fmt.Errorf("AirPlay name, IPv4 address, and port are required")
	}

	service, err := mdns.NewMDNSService(serviceName(name, mac), "_raop._tcp", "", hostName(mac), int(port), []net.IP{ip}, []string{
		"tp=UDP", "sm=false", "sv=false", "ek=1", "et=0,1", "md=0,1,2",
		"cn=0,1", "ch=2", "ss=16", "sr=44100", "vn=3", "txtvers=1",
		"am=AirPort10,115",
	})
	if err != nil {
		return nil, fmt.Errorf("create AirPlay mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, fmt.Errorf("start AirPlay mDNS: %w", err)
	}
	return &Advertiser{server: server}, nil
}

func (a *Advertiser) Close() {
	if a != nil && a.server != nil {
		a.server.Shutdown()
	}
}

func serviceName(name string, mac [6]byte) string {
	id := fmt.Sprintf("%02X%02X%02X%02X%02X%02X@%s", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5], name)
	for len(id) > 63 {
		id = strings.TrimSuffix(id, string(id[len(id)-1]))
	}
	return id
}

func hostName(mac [6]byte) string {
	return fmt.Sprintf("raop-%02x%02x%02x%02x%02x%02x.local.", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

func LANIPv4() (net.IP, error) {
	// ponytail: one default-route LAN; add explicit interface selection for multi-LAN hosts.
	conn, err := net.Dial("udp4", "192.0.2.1:80")
	if err != nil {
		return nil, fmt.Errorf("find LAN IPv4: %w", err)
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP.To4() == nil {
		return nil, fmt.Errorf("find LAN IPv4")
	}
	return addr.IP.To4(), nil
}
