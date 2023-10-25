package p2p

import (
	"context"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
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
	NextSeqNo int
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

func CreateWitnessingConnection(connID uuid.UUID, sender, receiver string) *Connection {
	return &Connection{
		ID: connID,
		Sender: sender,
		Witness: GetFullAddr(),
		Receiver: receiver,
		Status: ConnectionStatusOpen,
	}
}

func OpenStream(targetNode string, protocol protocol.ID) (network.Stream, error) {
	info, err := AddPeer(targetNode)
	if err != nil {
		return nil, err
	}
	return GetLibp2pNode().NewStream(context.Background(), info.ID, protocol)
}