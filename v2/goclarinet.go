package v2

import (
	"fmt"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/data"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
	"github.com/arobie1992/go-clarinet/v2/transport"
)

type Node struct {
	peerStore       peer.PeerStore
	connectionStore connection.ConnectionStore
	messageStore    data.MessageStore

	reputationStore reputation.ReputationStore
	trusts          reputation.TrustFunction

	transport transport.Transport
	log       log.Logger
}

func NewNode(
	peerStore peer.PeerStore,
	connectionStore connection.ConnectionStore,
	messageStore data.MessageStore,
	reputationStore reputation.ReputationStore,
	trustFunc reputation.TrustFunction,
	transport transport.Transport,
	handlers transport.Handlers,
	log log.Logger,
) (*Node, error) {
	node := Node{
		peerStore,
		connectionStore,
		messageStore,
		reputationStore,
		trustFunc,
		transport,
		log,
	}
	connectHandler := connectHandlerAdapter{handlers.ConnectHandler, &node}
	witnessHandler := witnessHandlerAdapter{handlers.WitnessHandler, &node}
	witnessNotificationHandler := witnessNotificationHandlerAdapter{handlers.WitnessNotificationHandler, &node}
	closeHandler := closeHandlerAdapter{handlers.CloseHandler, &node}
	if err := transport.RegisterHandlers(&connectHandler, &witnessHandler, &witnessNotificationHandler, &closeHandler); err != nil {
		return nil, err
	}
	return &node, nil
}

func (n *Node) Self() (peer.Peer, error) {
	return n.peerStore.Self()
}

// UpdatePeer updates the peer within the PeerStore. If the peer does not yet exist, it will add the peer to the peer store.
//
// This operation does not guarantee atomicity so modifications may have been made even if an error is returned.
func (n *Node) UpdatePeer(peer peer.Peer) error {
	for _, addr := range peer.Addresses() {
		if err := n.peerStore.AddAddr(peer.ID(), addr); err != nil {
			return err
		}
	}
	return nil
}

// Connect establishes an outgoing connection to the specified peer and attempts to set up the connection as far as possible.
func (n *Node) Connect(receiver peer.Peer, connOptions connection.Options, transportOptions transport.Options) error {
	if err := n.UpdatePeer(receiver); err != nil {
		return err
	}

	self, err := n.peerStore.Self()
	if err != nil {
		return err
	}

	connID, err := n.connectionStore.Create(self, receiver, connection.RequestingReceiver())
	if err != nil {
		return err
	}

	return n.connectionStore.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		receiver, err := n.peerStore.Find(conn.Receiver())
		if err != nil {
			return nil, err
		}
		if err := n.requestConnection(receiver, conn, connOptions, transportOptions); err != nil {
			return nil, err
		}

		conn, err = conn.SetStatus(connection.RequestingWitness())
		if err != nil {
			return conn, err
		}

		return n.handleWitnessSelection(conn, connOptions, transportOptions)
	})
}

func (n *Node) handleWitnessSelection(
	conn connection.Connection,
	connectionOptions connection.Options,
	transportOptions transport.Options,
) (connection.Connection, error) {
	self, err := n.peerStore.Self()
	if err != nil {
		return nil, err
	}

	var witness peer.Peer = nil
	switch connectionOptions.WitnessSelector {
	case connection.WitnessSelectorSender():
		if self.ID() != conn.Sender() {
			return conn.SetStatus(connection.AwaitingWitness())
		}
		witness, err = n.findWitness(conn, connectionOptions, transportOptions)
	case connection.WitnessSelectorReceiver():
		if self.ID() != conn.Receiver() {
			return conn.SetStatus(connection.AwaitingWitness())
		}
		witness, err = n.findWitness(conn, connectionOptions, transportOptions)
	default:
		return nil, fmt.Errorf(
			"Unrecognized WitnessSelector value %s. Supported values are [ %s, %s ]",
			connectionOptions.WitnessSelector,
			connection.WitnessSelectorSender(),
			connection.WitnessSelectorReceiver(),
		)
	}

	if err != nil {
		return nil, err
	}

	conn, err = conn.SetWitness(witness.ID())
	if err != nil {
		return conn, err
	}

	conn, err = conn.SetStatus(connection.NotifyingOfWitness())
	if err != nil {
		return conn, err
	}

	if err := n.sendWitnessNotification(conn, transportOptions); err != nil {
		return conn, err
	}

	return conn.SetStatus(connection.Open())
}

func (n *Node) sendWitnessNotification(conn connection.Connection, options transport.Options) error {
	if conn.Witness() == nil {
		return fmt.Errorf("Attempting to notify of witness for connection %s with no witness assigned", conn.ID())
	}

	self, err := n.peerStore.Self()
	if err != nil {
		return err
	}

	var peerToNotify peer.ID
	if conn.Sender() == self.ID() {
		peerToNotify = conn.Receiver()
	} else if conn.Receiver() == self.ID() {
		peerToNotify = conn.Sender()
	} else {
		panic("Self is neither sender nor receiver for connection %s.")
	}

	peer, err := n.peerStore.Find(peerToNotify)
	if err != nil {
		return err
	}

	return n.transport.Send(peer, options, connection.WitnessNotification{ConnectionID: conn.ID(), Witness: conn.Witness()})
}

func (n *Node) requestConnection(dest peer.Peer, conn connection.Connection, connOpts connection.Options, trptOpts transport.Options) error {
	connectRequest := connection.ConnectRequest{
		ConnID:   conn.ID(),
		Sender:   conn.Sender(),
		Receiver: conn.Receiver(),
		Options:  connOpts,
	}

	var connectResponse connection.ConnectResponse
	if _, err := n.transport.Exchange(dest, trptOpts, connectRequest, &connectResponse); err != nil {
		return err
	}

	if connectResponse.ConnID != conn.ID() {
		return fmt.Errorf("ConnectResponse contained the incorrect connection ID. Got: %s, Expected: %s", connectResponse.ConnID, conn.ID())
	}

	if connectResponse.Errors != nil && len(connectResponse.Errors) > 0 {
		return fmt.Errorf("Encountered the following errors while requesting the connection: %v", connectResponse.Errors)
	}

	if connectResponse.Accepted {
		return nil
	} else {
		return &connection.ConnectRejectError{ConnID: connectResponse.ConnID, Reasons: connectResponse.RejectReasons}
	}
}

func (n *Node) witnessCandidates(conn connection.Connection) ([]peer.Peer, error) {
	peers, err := n.peerStore.All()
	if err != nil {
		return []peer.Peer{}, err
	}

	witnessCandidates := []peer.Peer{}
	for _, p := range peers {
		if p.ID() == conn.Receiver() || p.ID() == conn.Sender() {
			continue
		}

		err := n.reputationStore.ReadOperation(p.ID(), func(rep reputation.Reputation) error {
			trusted, err := n.trusts(rep)
			if err != nil {
				return err
			}
			if trusted {
				witnessCandidates = append(witnessCandidates, p)
			}
			return nil
		})

		if err != nil {
			n.log.Error("Encountered error while assessing reputation for peer %s: %s", p.ID(), err)
		}
	}

	return witnessCandidates, nil
}

func (n *Node) findWitness(conn connection.Connection, connOpts connection.Options, trptOpts transport.Options) (peer.Peer, error) {
	witnessCandidates, err := n.witnessCandidates(conn)
	if err != nil {
		return nil, err
	}

	for _, p := range witnessCandidates {
		err := n.requestWitness(p, conn, connOpts, trptOpts)
		if err != nil {
			n.log.Error("Encountered error while requesting peer %s as witness for connection %s: %s", p.ID(), conn.ID(), err)
			continue
		}
		return p, nil
	}

	return nil, fmt.Errorf("Failed to find witness for connection %s", conn.ID())
}

func (n *Node) CloseConnection(connID connection.ID, transportOptions transport.Options) error {
	return n.connectionStore.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		if conn.Status() == connection.Closed() {
			return nil, nil
		}

		self, err := n.peerStore.Self()
		if err != nil {
			return nil, err
		}

		errs := []error{}

		participants := []peer.ID{conn.Sender(), conn.Witness(), conn.Receiver()}
		for _, id := range participants {
			if id == self.ID() || id == nil {
				continue
			}

			p, err := n.peerStore.Find(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("Encountered error while retrieving peer %s: %s", id, err))
				continue
			}

			if err := n.requestClose(p, conn, transportOptions); err != nil {
				errs = append(errs, fmt.Errorf("Encountered error sending close request to peer %s: %s", p.ID(), err))
			}
		}

		if len(errs) != 0 {
			// Change status to closing so that the system knows that it should not be treated as open, but that the close didn't completely succeed.
			conn, err = conn.SetStatus(connection.Closing())
			errs = append(errs, err)
			return conn, &connection.CloseError{ConnID: conn.ID(), Errors: errs}
		}
		return conn.SetStatus(connection.Closed())
	})
}

func (n *Node) requestClose(peer peer.Peer, conn connection.Connection, options transport.Options) error {
	closeRequest := connection.CloseRequest{ConnID: conn.ID()}
	return n.transport.Send(peer, options, closeRequest)
}

func (n *Node) requestWitness(peer peer.Peer, conn connection.Connection, connOpts connection.Options, trptOpts transport.Options) error {
	witnessRequest := connection.WitnessRequest{
		ConnID:   conn.ID(),
		Sender:   conn.Sender(),
		Receiver: conn.Receiver(),
		Options:  connOpts,
	}

	var witnessResponse connection.WitnessResponse
	_, err := n.transport.Exchange(peer, trptOpts, witnessRequest, &witnessResponse)
	if err != nil {
		return err
	}

	if witnessResponse.ConnID != conn.ID() {
		return fmt.Errorf("WitnessResponse contained the incorrect connection ID. Got: %s, Expected: %s", witnessResponse.ConnID, conn.ID())
	}

	if witnessResponse.Errors != nil && len(witnessResponse.Errors) > 0 {
		return fmt.Errorf("Encountered the following errors while requesting witness: %v", witnessResponse.Errors)
	}

	if witnessResponse.Accepted {
		return nil
	} else {
		return &connection.WitnessRejectError{ConnID: witnessResponse.ConnID, Reasons: witnessResponse.RejectReasons}
	}
}

type connectHandlerAdapter struct {
	userHandler transport.ConnectHandler
	node        *Node
}

func (h *connectHandlerAdapter) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	sender, err := h.node.peerStore.Find(request.Sender)
	if err != nil {
		return connection.ConnectResponse{
			ConnID:        request.ConnID,
			Errors:        []error{err},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.ConnectResponse{
			ConnID:        request.ConnID,
			Errors:        []error{err},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.ConnectResponse{
			ConnID:        request.ConnID,
			Errors:        []error{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}

	self, err := h.node.peerStore.Self()
	if err != nil {
		return connection.ConnectResponse{
			ConnID:        request.ConnID,
			Errors:        []error{err},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() != self.ID() && receiver.ID() != self.ID() {
		return connection.ConnectResponse{
			ConnID:        request.ConnID,
			Errors:        []error{},
			Accepted:      false,
			RejectReasons: []string{"Connection is not to or from this node."},
		}, nil
	}

	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil || resp.Errors != nil || len(resp.Errors) > 0 || !resp.Accepted {
		return resp, err
	}

	if err := h.node.connectionStore.Accept(request.ConnID, sender, receiver, connection.RequestingWitness()); err != nil {
		if resp.Errors == nil {
			resp.Errors = []error{}
		}
		resp.Errors = append(resp.Errors, err)
		return resp, nil
	}

	if request.Options.WitnessSelector == connection.WitnessSelectorReceiver() && self.ID() == request.Receiver ||
		request.Options.WitnessSelector == connection.WitnessSelectorSender() && self.ID() == request.Sender {
		go func() {
			err := h.node.connectionStore.Update(request.ConnID, func(conn connection.Connection) (connection.Connection, error) {
				return h.node.handleWitnessSelection(conn, request.Options, h.Options())
			})
			if err != nil {
				h.node.log.Error("Encountered error while attempting to select witness for connection %s: %s", request.ConnID, err)
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
			ConnID:        request.ConnID,
			Errors:        []error{err},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.WitnessResponse{
			ConnID:        request.ConnID,
			Errors:        []error{err},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.WitnessResponse{
			ConnID:        request.ConnID,
			Errors:        []error{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}

	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil || resp.Errors != nil || len(resp.Errors) > 0 || !resp.Accepted {
		return resp, err
	}

	if err := h.node.connectionStore.Accept(request.ConnID, sender, receiver, connection.Open()); err != nil {
		if resp.Errors == nil {
			resp.Errors = []error{}
		}
		resp.Errors = append(resp.Errors, err)
		return resp, nil
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
				"Received witness notification from peer %s for connection %s that is not await witness notification.",
				peerID,
				conn.ID(),
			)
		}
		self, err := h.node.peerStore.Self()
		if err != nil {
			return nil, err
		}

		expectedPeer := conn.Sender()
		if expectedPeer == self.ID() {
			expectedPeer = conn.Receiver()
		}

		if peerID != expectedPeer {
			return nil, fmt.Errorf(
				"Received witness notification from unexpected peer for connection %s. Expected: %s, Got: %s",
				conn.ID(),
				expectedPeer,
				peerID,
			)
		}

		return conn.SetWitness(notification.Witness)
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
	return h.node.connectionStore.Update(request.ConnID, func(conn connection.Connection) (connection.Connection, error) {
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
