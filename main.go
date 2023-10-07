package main

import (
	"log"
	"os"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/control"
	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
)

func main() {
	configPath := ""
	if len(os.Args) == 2 {
		configPath = os.Args[1]
	}
	config, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load configuration.", err)
	}

	if err := p2p.InitLibp2pNode(config); err != nil {
		log.Fatal("Failed to initialize libp2p node", err)
	}
	log.Printf("I am %s\n", p2p.GetFullAddr())

	if err := repository.InitDB(config); err != nil {
		log.Fatal("Failed to initialize database", err)
	}

	// start a http handler so we have some endpoints to trigger behavior through for testing
	if err := control.StartAdminServer(config); err != nil {
		log.Fatal("Failed to start server", err)
	}
}
