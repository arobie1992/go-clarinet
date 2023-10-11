package p2p

import (
	"github.com/google/uuid"
)

type ConnectionStatus int

const (
	ConnectionStatusRequestingReceiver = iota
	ConnectionStatusRequestingWitness
	ConnectionStatusOpen
	ConnectionStatusClosed
)

type Connection struct {
	ID       uuid.UUID `gorm:"primaryKey"`
	Sender   string
	Witness  string
	Receiver string
	Status   ConnectionStatus
}

func CreateOutgoingConnection(targetNode string) (*Connection, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	return &Connection{
		ID:       id,
		Sender:   GetFullAddr(),
		Witness:  "",
		Receiver: targetNode,
		Status:   ConnectionStatusRequestingReceiver,
	}, nil
}

func CreateIncomingConnection(connID uuid.UUID, sender string) *Connection {
	return &Connection{
		ID:       connID,
		Sender:   sender,
		Witness:  "",
		Receiver: GetFullAddr(),
		Status:   ConnectionStatusRequestingWitness,
	}
}
