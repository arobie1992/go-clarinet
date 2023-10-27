package main

import (
	"os"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/control"
	"github.com/go-clarinet/cryptography"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
	"github.com/go-clarinet/reputation"
)

func main() {
	log.InitLogger()

	configPath := ""
	if len(os.Args) == 2 {
		configPath = os.Args[1]
	}
	config, err := config.LoadConfig(configPath)
	if err != nil {
		log.Log().Fatalf("Failed to load configuration: %s", err)
	}

	if err := cryptography.LoadPrivKeys(config.Libp2p.CertPath); err != nil {
		log.Log().Fatalf("Failed to initialize private keys: %s", err)
	}

	if err := p2p.InitLibp2pNode(config); err != nil {
		log.Log().Fatalf("Failed to initialize libp2p node: %s", err)
	}
	log.Log().Infof("I am %s", p2p.GetFullAddr())

	if err := repository.InitDB(config, &p2p.Connection{}, &p2p.DataMessage{}, &reputation.ReputationInfo{}); err != nil {
		log.Log().Fatalf("Failed to initialize database: %s", err)
	}

	// start a http handler so we have some endpoints to trigger behavior through for testing
	if err := control.StartAdminServer(config); err != nil {
		log.Log().Fatalf("Failed to start server: %s", err)
	}
}
