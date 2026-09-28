package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hkfuertes/goplay2-sendspin/internal/bridge"
)

func main() {
	portBase := flag.Uint("port-base", 7000, "first AirPlay port")
	portRange := flag.Uint("port-range", 10, "ports reserved per AirPlay target")
	flag.Parse()

	manager, err := bridge.New(bridge.Config{PortBase: uint16(*portBase), PortRange: uint16(*portRange)})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := manager.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
