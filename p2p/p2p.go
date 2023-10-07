package p2p

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/control"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
)

var libp2pNode host.Host
var fullAddr string
var once sync.Once

func GetLibP2pNode() host.Host {
	return libp2pNode
}

func GetFullAddr() string {
	return fullAddr
}

func InitLibp2pNode(config *config.Config) error {
	var retErr error = nil
	once.Do(func() {
		priv, err := getKey(config.Libp2p.CertPath)
		if err != nil {
			retErr = err
			return
		}

		node, err := libp2p.New(
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", config.Libp2p.Port)),
			libp2p.Identity(*priv),
			libp2p.DisableRelay(),
		)
		if err != nil {
			retErr = err
			return
		}

		node.SetStreamHandler(control.ConnectProtocolID, control.ConnectStreamHandler)
		if err := control.SetLibp2pNode(node); err != nil {
			retErr = err
		}
		fullAddr = getHostAddress(control.GetLibP2pNode())
	})
	return retErr
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
