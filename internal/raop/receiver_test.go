package raop

import (
	"net"
	"testing"

	"github.com/hkfuertes/goplay2-sendspin/internal/pcm"
)

func TestReceiverStartsAndStops(t *testing.T) {
	r, err := New(Config{
		Name:   "sendspin test",
		MAC:    [6]byte{2, 0, 0, 0, 0, 1},
		Host:   net.IPv4(127, 0, 0, 1),
		Source: pcm.New(882),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Port() == 0 {
		t.Fatal("receiver has no RTSP port")
	}
	r.Close()
}
