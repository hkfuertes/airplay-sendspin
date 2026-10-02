// Package main builds libgolibrespot.so, the CFFI boundary around go-librespot:
// each gl_start runs one Spotify Connect device inside the bridge process.
package main

import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
	"github.com/sirupsen/logrus"
)

func main() {}

type device struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	mu      sync.Mutex
	devices = map[int]*device{}
	lastID  int
)

// gl_start writes s16le 44.1 kHz stereo PCM to pcmFd and one event per line to eventFd:
// go-librespot API event types, "register <port> <txt>..." and "unregister". The caller
// keeps both pipes open until gl_stop returns. Returns a handle, or -1.
//
//export gl_start
func gl_start(name, deviceID, statePath *C.char, pcmFd, eventFd C.int) C.int {
	events, err := os.OpenFile(fmt.Sprintf("/dev/fd/%d", eventFd), os.O_WRONLY, 0)
	if err != nil {
		return -1
	}
	sink := &eventSink{file: events}
	log := logger{logrus.WithField("device", C.GoString(name))}
	store := fileStore(C.GoString(statePath))
	cfg := &daemon.Config{
		DeviceId:              C.GoString(deviceID),
		DeviceName:            C.GoString(name),
		DeviceType:            "speaker",
		AudioBackend:          "pipe",
		AudioOutputPipe:       fmt.Sprintf("/dev/fd/%d", pcmFd), // opened lazily, hence the open caller pipe
		AudioOutputPipeFormat: "s16le",
		Bitrate:               320,
		VolumeSteps:           100,
		InitialVolume:         100,
		SkipDebounce:          600 * time.Millisecond,
		ZeroconfEnabled:       true,
		ImageSize:             "default",
		Credentials: daemon.CredentialsConfig{
			Type:     "zeroconf",
			Zeroconf: daemon.ZeroconfCredentials{PersistCredentials: true},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &device{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(d.done)
		defer events.Close()
		for ctx.Err() == nil {
			app, err := daemon.New(&daemon.Options{
				Logger: log, Config: cfg, StateStore: store, APIServer: sink, ZeroconfRegistrar: sink,
			})
			if err == nil {
				err = app.Run(ctx)
			}
			if ctx.Err() != nil {
				return
			}
			// ponytail: fixed retry (e.g. no internet at boot); add backoff if Spotify rate-limits it.
			log.WithError(err).Warn("go-librespot stopped, retrying in 5s")
			sink.write("stopped")
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}()
	mu.Lock()
	defer mu.Unlock()
	lastID++
	devices[lastID] = d
	return C.int(lastID)
}

// gl_stop stops a device and waits (up to 5 s) until it no longer touches the caller's pipes.
//
//export gl_stop
func gl_stop(id C.int) {
	mu.Lock()
	d := devices[int(id)]
	delete(devices, int(id))
	mu.Unlock()
	if d == nil {
		return
	}
	d.cancel()
	select {
	case <-d.done:
	case <-time.After(5 * time.Second):
	}
}

// eventSink is both the daemon's API server and its mDNS registrar: the bridge
// receives events and publishes the service with python-zeroconf.
type eventSink struct {
	mu   sync.Mutex
	file *os.File
}

func (s *eventSink) write(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.file.WriteString(line + "\n")
}

func (s *eventSink) Emit(ev *daemon.ApiEvent)          { s.write(string(ev.Type)) }
func (s *eventSink) Receive() <-chan daemon.ApiRequest { return nil } // ponytail: no control requests yet
func (s *eventSink) SetAuthCode(*daemon.ApiDeviceAuth) {}
func (s *eventSink) Close() error                      { return nil }
func (s *eventSink) UpdateName(string) error           { return nil }
func (s *eventSink) Shutdown()                         { s.write("unregister") }
func (s *eventSink) Register(_, _, _ string, port int, txt []string) error {
	s.write(fmt.Sprintf("register %d %s", port, strings.Join(txt, " ")))
	return nil
}

// fileStore persists go-librespot's state (device ID, zeroconf credentials, volume) as JSON.
type fileStore string

func (f fileStore) Load() (*librespot.AppState, error) {
	state := &librespot.AppState{}
	data, err := os.ReadFile(string(f))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	} else if err != nil {
		return nil, err
	}
	return state, json.Unmarshal(data, state)
}

func (f fileStore) Save(state *librespot.AppState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.WriteFile(string(f)+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(string(f)+".tmp", string(f))
}

type logger struct{ *logrus.Entry }

func (l logger) WithField(key string, value interface{}) librespot.Logger {
	return logger{l.Entry.WithField(key, value)}
}

func (l logger) WithError(err error) librespot.Logger { return logger{l.Entry.WithError(err)} }
