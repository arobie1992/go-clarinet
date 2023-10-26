package cryptography

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"

	lc "github.com/libp2p/go-libp2p/core/crypto"
)

var rsaPrivKey *rsa.PrivateKey = nil
var privKey *lc.PrivKey = nil

func RSAPrivKey() (*rsa.PrivateKey, error) {
	if rsaPrivKey == nil {
		return nil, errors.New("No private key loaded")
	}
	return rsaPrivKey, nil
}

func PrivKey() (*lc.PrivKey, error) {
	if privKey == nil {
		return nil, errors.New("No private key loaded")
	}
	return privKey, nil
}

func LoadPrivKeys(file string) error {
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
		return err
	}
	rsaPrivKey = key.(*rsa.PrivateKey)

	pk, _, err := lc.KeyPairFromStdKey(key)

	privKey = &pk
	return nil
}

func Sign(str string) (string, error) {
	h := sha256.Sum256([]byte(str))
	pk, err := RSAPrivKey()
	if err != nil {
		return "", err
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return string(sig), nil
}
