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
	configPath := flag.String("config", "config.xml", "persistent speaker registry")
	serverPort := flag.Uint("server-port", 8927, "inbound Sendspin server port")
	serverName := flag.String("server-name", "AirPlay Sendspin", "inbound Sendspin server name")
	flag.Parse()

	manager, err := bridge.New(bridge.Config{
		PortBase: uint16(*portBase), PortRange: uint16(*portRange), ConfigPath: *configPath,
		ServerPort: uint16(*serverPort), ServerName: *serverName,
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := manager.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
