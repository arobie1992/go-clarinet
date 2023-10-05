package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-clarinet/config"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configPath := ""
	if len(os.Args) == 2 {
		configPath = os.Args[1]
	}
	config, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load configuration.", err)
	}

	priv, err := getKey(config.CertPath)
	if err != nil {
		log.Fatal("Failed to load certificate.", err)
	}

	node, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", config.Port)),
		libp2p.Identity(*priv),
		libp2p.DisableRelay(),
	)
	if err != nil {
		log.Fatal("Failed to create node.", err)
	}

	log.Println(node.Addrs())

	<-ctx.Done()
}

func getKey(file string) (*crypto.PrivKey, error) {
	r, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	key, _, err := crypto.GenerateEd25519Key(r)
	if err != nil {
		return nil, err
	}
	return &key, nil
}
