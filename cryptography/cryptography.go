package cryptography

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var privKey *crypto.PrivKey = nil

func PrivKey() (*crypto.PrivKey, error) {
	if privKey == nil {
		return nil, errors.New("No private key loaded")
	}
	return privKey, nil
}

func LoadPrivKey(file string) error {
	r, err := os.Open(file)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("Invalid key format")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return errors.New("Provided Key was neither PKCS1 nor PKCS8. Please use one of those formats.")
		}
	}

	pk, _, err := crypto.KeyPairFromStdKey(key)

	privKey = &pk
	return nil
}

func InitPrivKeyFromNode(node host.Host, selfAddr string) error {
	maddr, err := multiaddr.NewMultiaddr(selfAddr)
	if err != nil {
		return err
	}

	nodeID, err := maddr.ValueForProtocol(multiaddr.P_P2P)
	if err != nil {
		return err
	}

	peerID, err := peer.Decode(nodeID)
	if err != nil {
		return err
	}

	key := node.Peerstore().PrivKey(peerID)
	if key == nil {
		return errors.New(fmt.Sprintf("No key for peer %s", peerID))
	}
	privKey = &key
	return nil
}

func Sign(str string) (string, error) {
	sig, err := (*privKey).Sign([]byte(str))
	if err != nil {
		return "", err
	}
	return string(sig), nil
}
