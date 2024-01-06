package crypto

type PrivateKey interface {
	Sign(data []byte) ([]byte, error)
}

type PublicKey interface {
	Verify(data, sig []byte) (bool, error)
}
