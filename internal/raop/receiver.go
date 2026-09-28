// Package raop owns the patched libraop AirPlay 1 receiver boundary.
package raop

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/libraop/src -I${SRCDIR}/../../third_party/libraop/src/inc -I${SRCDIR}/../../third_party/libraop/crosstools/src -I${SRCDIR}/../../third_party/libraop/dmap-parser -I${SRCDIR}/../../third_party/libraop/libmdns/targets/include/mdnssvc -I${SRCDIR}/../../third_party/libraop/libmdns/targets/include/mdnssd -I${SRCDIR}/../../third_party/libraop/libopenssl/targets/linux/x86_64/include -I${SRCDIR}/../../third_party/libraop/libcodecs/targets/include/addons -I${SRCDIR}/../../third_party/libraop/libcodecs/targets/include/flac -I${SRCDIR}/../../third_party/libraop/libcodecs/targets/include/shine -I${SRCDIR}/../../third_party/libraop/libcodecs/targets/include/faac
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/libraop/lib/linux/x86_64 -lraop -L${SRCDIR}/../../third_party/libraop/libcodecs/targets/linux/x86_64 -lcodecs -L${SRCDIR}/../../third_party/libraop/libmdns/targets/linux/x86_64 -lmdns -L${SRCDIR}/../../third_party/libraop/libopenssl/targets/linux/x86_64 -lopenssl -lstdc++ -lpthread -ldl -lm -latomic
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"net"
	"runtime/cgo"
	"sync"
	"unsafe"

	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
)

type Event uint8

const (
	EventPlay  Event = Event(C.BRIDGE_RAOP_PLAY)
	EventFlush Event = Event(C.BRIDGE_RAOP_FLUSH)
	EventStop  Event = Event(C.BRIDGE_RAOP_STOP)
)

type Config struct {
	Name      string
	MAC       [6]byte
	Host      net.IP
	PortBase  uint16
	PortRange uint16
	Source    pcm.Sink
	OnEvent   func(Event)
	OnVolume  func(float64)
}

// Receiver advertises no mDNS itself; its caller advertises the returned port.
type Receiver struct {
	source   pcm.Sink
	onEvent  func(Event)
	onVolume func(float64)
	handle   cgo.Handle
	c        *C.bridge_receiver_t
	once     sync.Once
}

func New(config Config) (*Receiver, error) {
	if config.Name == "" {
		return nil, fmt.Errorf("AirPlay name is required")
	}
	if config.Source == nil {
		return nil, fmt.Errorf("PCM source is required")
	}
	host := config.Host.To4()
	if host == nil {
		return nil, fmt.Errorf("AirPlay IPv4 host is required")
	}
	if config.PortBase != 0 && config.PortRange < 3 {
		return nil, fmt.Errorf("AirPlay port range must contain at least three ports")
	}
	if config.PortRange == 0 {
		config.PortRange = 1
	}

	r := &Receiver{source: config.Source, onEvent: config.OnEvent, onVolume: config.OnVolume}
	r.handle = cgo.NewHandle(r)

	name := C.CString(config.Name)
	mac := C.CBytes(config.MAC[:])
	cHost := C.CBytes(host)
	defer C.free(unsafe.Pointer(name))
	defer C.free(mac)
	defer C.free(cHost)

	r.c = C.bridge_receiver_new(name, (*C.uint8_t)(mac), (*C.uint8_t)(cHost), C.uint16_t(config.PortBase), C.uint16_t(config.PortRange), C.uintptr_t(r.handle))
	if r.c == nil {
		r.handle.Delete()
		return nil, fmt.Errorf("create AirPlay receiver")
	}
	if r.Port() == 0 {
		r.Close()
		return nil, fmt.Errorf("AirPlay receiver did not bind an RTSP port")
	}
	return r, nil
}

func (r *Receiver) Port() uint16 {
	if r == nil || r.c == nil {
		return 0
	}
	return uint16(C.bridge_receiver_port(r.c))
}

func (r *Receiver) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.c != nil {
			C.bridge_receiver_delete(r.c)
			r.c = nil
		}
		r.handle.Delete()
	})
}

//export goRaopPCM
func goRaopPCM(owner C.uintptr_t, samples *C.int16_t, frames C.size_t) {
	r := cgo.Handle(owner).Value().(*Receiver)
	r.source.PushS16(unsafe.Slice((*int16)(unsafe.Pointer(samples)), int(frames)*pcm.Channels))
}

//export goRaopEvent
func goRaopEvent(owner C.uintptr_t, event C.int) {
	r := cgo.Handle(owner).Value().(*Receiver)
	e := Event(event)
	switch e {
	case EventPlay, EventFlush, EventStop:
		r.source.Reset()
	}
	if r.onEvent != nil {
		r.onEvent(e)
	}
}

//export goRaopVolume
func goRaopVolume(owner C.uintptr_t, volume C.double) {
	r := cgo.Handle(owner).Value().(*Receiver)
	if r.onVolume != nil {
		r.onVolume(float64(volume))
	}
}
