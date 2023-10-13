package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/control"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
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

	priv, err := getKey(config.Libp2p.CertPath)
	if err != nil {
		log.Fatal("Failed to load certificate.", err)
	}

	node, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", config.Libp2p.Port)),
		libp2p.Identity(*priv),
		libp2p.DisableRelay(),
	)
	if err != nil {
		log.Fatal("Failed to create node.", err)
	}

	node.SetStreamHandler(control.ConnectProtocolID, control.ConnectStreamHandler)
	if err := control.SetLibp2pNode(node); err != nil {
		log.Fatal("Global libp2pNode already set!", err)
	}
	fullAddr := getHostAddress(control.GetLibP2pNode())
	log.Printf("I am %s\n", fullAddr)

	// start a http handler so we have some endpoints to trigger behavior through for testing
	http.HandleFunc(control.InitiateConnectionPath, control.InitiateConnection)
	log.Println("Starting http server")
	err = http.ListenAndServe(fmt.Sprintf(":%d", config.Admin.Port), nil)
	if err != nil {
		log.Fatal("Failed to start server", err)
	}
	<-ctx.Done()
}

func getKey(file string) (*crypto.PrivKey, error) {
	r, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("Invalid key format")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	libp2pKey, _, err := crypto.KeyPairFromStdKey(key)

	return &libp2pKey, nil
}

func getHostAddress(ha host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", ha.ID()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := ha.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}
