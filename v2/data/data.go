package data

import (
	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/crypto"
	"github.com/arobie1992/go-clarinet/v2/peer"
)

type Message interface {
	ConnectionID() connection.ID
	SequenceNumber() int
	Data() []byte
	SenderSign(privateKey crypto.PrivateKey) (Message, error)
	VerifySender(publicKey crypto.PublicKey) (bool, error)
	WitnessSign(privateKey crypto.PrivateKey) (Message, error)
	VerifyWitness(publicKey crypto.PublicKey) (bool, error)
}

type MessageStore interface {
	Add(message Message) error
	Find(connectionID connection.ID, sequenceNumber int) (Message, error)
	All() ([]Message, error)
}

type Query interface {
	ConnectionID() connection.ID
	SequenceNumber() int
}

type Answer interface {
	ConnectionID() connection.ID
	SequenceNumber() int
	MessageHash() []byte
	SenderSign(privateKey crypto.PrivateKey) error
	VerifySenderAnswer(message Message, publicKey crypto.PublicKey) (bool, error)
	Sign(privateKey crypto.PrivateKey) error
	VerifyAnswer(message Message, publicKey crypto.PublicKey) (bool, error)
}

type Forward interface {
	ConnectionID() connection.ID
	SequenceNumber() int
	MessageHash() []byte
	QueryiedNode() peer.ID
	VerifySenderAnswer(message Message, publickKey crypto.PublicKey) (bool, error)
	VerifyAnswer(message Message, publickKey crypto.PublicKey) (bool, error)
	ForwarderSign(privateKey crypto.PrivateKey) error
	VerifyForwarder(publicKey crypto.PublicKey) (bool, error)
}
