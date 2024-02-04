package v2

import (
	"errors"
	"fmt"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/peer"
)

type readOnlyConnection struct {
	id       connection.ID
	sender   peer.ID
	witness  peer.ID
	receiver peer.ID
	status   connection.Status
}

func (c readOnlyConnection) String() string {
	return fmt.Sprintf("{%s %s %s %s %s}", c.id, c.sender, c.witness, c.receiver, c.status)
}

func (c readOnlyConnection) ID() connection.ID {
	return c.id
}

func (c readOnlyConnection) Receiver() peer.ID {
	return c.receiver
}

func (c readOnlyConnection) Sender() peer.ID {
	return c.sender
}

func (_ readOnlyConnection) SetStatus(status connection.Status) (connection.Connection, error) {
	return nil, errors.ErrUnsupported
}

func (_ readOnlyConnection) SetWitness(peerID peer.ID) (connection.Connection, error) {
	return nil, errors.ErrUnsupported
}

func (c readOnlyConnection) Status() connection.Status {
	return c.status
}

func (c readOnlyConnection) Witness() peer.ID {
	return c.witness
}
