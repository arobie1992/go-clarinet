package cryptography

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
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

func Sign(str string) (string, error) {
	sig, err := (*privKey).Sign([]byte(str))
	if err != nil {
		return "", err
	}
	return string(sig), nil
}
