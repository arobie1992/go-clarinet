package v2

import (
	"fmt"
	"math/rand"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/transport"
)

type connectHandlerAdapter struct {
	userHandler transport.ConnectHandler
	node        *Node
}

func (h *connectHandlerAdapter) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	sender, err := h.node.peerStore.Find(request.Sender)
	if err != nil {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Found sender in peerstore: %s", sender)

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Found receiver in peerstore: %s", receiver)

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}
	h.node.log.Trace("ConnectRequest is from a node that is either going to be the sender or the receiver, so is permitted.")

	self, err := h.node.peerStore.Self()
	if err != nil {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Got self: %s", self)

	if request.Sender != self.ID() && request.Receiver != self.ID() {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Connection is not to or from this node."},
		}, nil
	}
	h.node.log.Trace("ConnectRequest is for a connection that would either be to or from this node, so is permitted.")

	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil || len(resp.Errors) > 0 || !resp.Accepted {
		h.node.log.Debug("Connection will not be added to peer store")
		if h.node.log.Level().AtLeast(log.Trace()) {
			h.node.log.Trace("err != nil: %t", err != nil)
			h.node.log.Trace("len(resp.Errors): %d", len(resp.Errors))
			h.node.log.Trace("!resp.Accepted: %t", !resp.Accepted)
		}
		return resp, err
	}
	h.node.log.Trace("Got response from handler: %v", resp)

	h.node.log.Debug("Preparing to accept connection %s into peerstore", request.ConnectionID)
	if err := h.node.connectionStore.Accept(request.ConnectionID, sender, receiver, connection.AwaitingWitness()); err != nil {
		resp.Errors = append(resp.Errors, err.Error())
		return resp, nil
	}

	if (request.Options.WitnessSelector == connection.WitnessSelectorReceiver() && self.ID() == request.Receiver) ||
		(request.Options.WitnessSelector == connection.WitnessSelectorSender() && self.ID() == request.Sender) {
		go func() {
			err := h.node.connectionStore.Update(request.ConnectionID, func(conn connection.Connection) (connection.Connection, error) {
				return h.node.handleWitnessSelection(conn, request.Options, h.Options())
			})
			if err != nil {
				h.node.log.Error("Encountered error while attempting to select witness for connection %s: %s", request.ConnectionID, err)
			}
		}()
	}
	return resp, nil
}

func (h *connectHandlerAdapter) Options() transport.Options {
	return h.userHandler.Options()
}

type witnessHandlerAdapter struct {
	userHandler transport.WitnessHandler
	node        *Node
}

func (h *witnessHandlerAdapter) Handle(peerID peer.ID, request connection.WitnessRequest) (connection.WitnessResponse, error) {
	sender, err := h.node.peerStore.Find(request.Sender)
	if err != nil {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}

	self, err := h.node.Self()
	if err != nil {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() == self.ID() || receiver.ID() == self.ID() {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Cannot witness request in self is sender or receiver in."},
		}, nil
	}

	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil || len(resp.Errors) > 0 || !resp.Accepted {
		return resp, err
	}

	if err := h.node.connectionStore.Accept(request.ConnectionID, sender, receiver, connection.Open()); err != nil {
		resp.Errors = append(resp.Errors, err.Error())
		return resp, nil
	}

	err = h.node.connectionStore.Update(request.ConnectionID, func(conn connection.Connection) (connection.Connection, error) {
		self, err := h.node.Self()
		if err != nil {
			return nil, err
		}
		return conn.SetWitness(self.ID())
	})
	if err != nil {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnectionID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	return resp, nil
}

func (h *witnessHandlerAdapter) Options() transport.Options {
	return h.userHandler.Options()
}

type witnessNotificationHandlerAdapter struct {
	userHandler transport.WitnessNotificationHandler
	node        *Node
}

func (h *witnessNotificationHandlerAdapter) Handle(peerID peer.ID, notification connection.WitnessNotification) error {
	return h.node.connectionStore.Update(notification.ConnectionID, func(conn connection.Connection) (connection.Connection, error) {
		if conn.Status() != connection.AwaitingWitness() {
			return nil, fmt.Errorf(
				"Received witness notification from peer %s for connection %s that is not awaiting witness notification.",
				peerID,
				conn.ID(),
			)
		}
		h.node.log.Trace("Connection %s is in %s so will update", conn.ID(), connection.AwaitingWitness())

		self, err := h.node.peerStore.Self()
		if err != nil {
			return nil, err
		}
		h.node.log.Trace("Successfully got self")

		expectedPeer := conn.Sender()
		if expectedPeer == self.ID() {
			expectedPeer = conn.Receiver()
		}
		h.node.log.Debug("Expecting witness notification from: %s", expectedPeer)

		if peerID != expectedPeer {
			return nil, fmt.Errorf(
				"Received witness notification from unexpected peer for connection %s. Expected: %s, Got: %s",
				conn.ID(),
				expectedPeer,
				peerID,
			)
		}
		h.node.log.Trace("Witness notification came from expected peer")

		if err := h.userHandler.Handle(peerID, notification); err != nil {
			return nil, err
		}

		conn, err = conn.SetWitness(notification.Witness)
		if err != nil {
			return conn, err
		}
		return conn.SetStatus(connection.Open())
	})
}

func (h *witnessNotificationHandlerAdapter) Options() transport.Options {
	return h.userHandler.Options()
}

type closeHandlerAdapter struct {
	userHandler transport.CloseHandler
	node        *Node
}

func (h *closeHandlerAdapter) Handle(peerID peer.ID, request connection.CloseRequest) error {
	return h.node.connectionStore.Update(request.ConnectionID, func(conn connection.Connection) (connection.Connection, error) {
		if conn.Status() == connection.Closed() {
			return nil, nil
		}

		if err := h.userHandler.Handle(peerID, request); err != nil {
			conn, err2 := conn.SetStatus(connection.Closing())
			if err2 != nil {
				h.node.log.Error("Encountered error while attempting to set connection %s status to %s: %s", conn.ID(), connection.Closing(), err)
				return nil, err
			}
			return conn, err
		}

		return conn.SetStatus(connection.Closed())
	})
}

func (h *closeHandlerAdapter) Options() transport.Options {
	return h.userHandler.Options()
}

type peerRequestHandlerAdapter struct {
	userHandler transport.PeerRequestHandler
	node        *Node
}

func (h *peerRequestHandlerAdapter) Handle(peerID peer.ID, request peer.PeersRequest) (*peer.PeersResponse, error) {
	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}
	peers, err := h.node.Peers()
	if err != nil {
		return nil, err
	}

	self, err := h.node.Self()
	if err != nil {
		return nil, err
	}

	resp = &peer.PeersResponse{}
	for i := 0; i < request.Num && len(peers) > 0; i += 1 {
		ind := rand.Intn(len(peers))
		p := peers[ind]
		peers = append(peers[:ind], peers[ind+1:]...)
		if p.ID() == self.ID() {
			ind := rand.Intn(len(peers))
			p = peers[ind]
			peers = append(peers[:ind], peers[ind+1:]...)
		}
		resp.Peers = append(resp.Peers, p)
	}
	return resp, nil
}

func (h *peerRequestHandlerAdapter) Options() transport.Options {
	return h.userHandler.Options()
}
