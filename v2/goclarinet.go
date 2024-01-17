package v2

import (
	"errors"
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

// Connections returns a read only view of all the conncetions in the node's peerstore
func (n *Node) Connections() ([]connection.Connection, error) {
	ids, err := n.connectionStore.All()
	if err != nil {
		return nil, err
	}
	n.log.Trace("Found connection IDs %v", ids)
	conns := []connection.Connection{}
	for _, id := range ids {
		err := n.connectionStore.Read(id, func(conn connection.Connection) error {
			c := readOnlyConnection{conn.ID(), conn.Sender(), conn.Witness(), conn.Receiver(), conn.Status()}
			conns = append(conns, c)
			return nil
		})
		if err != nil {
			n.log.Error("Encountered error while reading connection %s: %s", id, err)
		}
	}
	return conns, nil
}

func (n *Node) Peers() ([]peer.Peer, error) {
	return n.peerStore.All()
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
		if n.log.Level().AtLeast(log.Trace()) {
			p, err := n.peerStore.Find(peer.ID())
			if err != nil {
				n.log.Error("Error while retrieving peer to verify: %s", err)
			}
			n.log.Trace("Added peer: {%s %v}", p.ID(), p.Addresses())
		}
	}
	return nil
}

// Connect establishes an outgoing connection to the specified peer and attempts to set up the connection as far as possible.
func (n *Node) Connect(receiver peer.Peer, connOptions connection.Options, transportOptions transport.Options) (connection.ID, error) {
	if err := n.UpdatePeer(receiver); err != nil {
		return connection.ID{}, err
	}

	self, err := n.peerStore.Self()
	if err != nil {
		return connection.ID{}, err
	}

	connID, err := n.connectionStore.Create(self, receiver, connection.RequestingReceiver())
	if err != nil {
		return connection.ID{}, err
	}

	err = n.connectionStore.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		receiver, err := n.peerStore.Find(conn.Receiver())
		if err != nil {
			return nil, err
		}
		if err := n.requestConnection(receiver, conn, connOptions, transportOptions); err != nil {
			return nil, err
		}
		return n.handleWitnessSelection(conn, connOptions, transportOptions)
	})
	return connID, err
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

	switch connectionOptions.WitnessSelector {
	case connection.WitnessSelectorSender():
		if self.ID() != conn.Sender() {
			return conn.SetStatus(connection.AwaitingWitness())
		}
		conn, err = n.findWitness(conn, connectionOptions, transportOptions)
	case connection.WitnessSelectorReceiver():
		if self.ID() != conn.Receiver() {
			return conn.SetStatus(connection.AwaitingWitness())
		}
		conn, err = n.findWitness(conn, connectionOptions, transportOptions)
	default:
		err = fmt.Errorf(
			"Unrecognized WitnessSelector value %s. Supported values are [ %s, %s ]",
			connectionOptions.WitnessSelector,
			connection.WitnessSelectorSender(),
			connection.WitnessSelectorReceiver(),
		)
	}
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

	if connectResponse.ConnectionID != conn.ID() {
		return fmt.Errorf("ConnectResponse contained the incorrect connection ID. Expected: %s, Got: %s", conn.ID(), connectResponse.ConnectionID)
	}

	n.log.Trace("Preparing to check errors %v", connectResponse.Errors)
	for i, e := range connectResponse.Errors {
		n.log.Trace("Error %d is %s", i, e)
	}
	if len(connectResponse.Errors) > 0 {
		n.log.Trace("Errors == nil is %t", connectResponse.Errors == nil)
		n.log.Trace("len(Errors) is %d", len(connectResponse.Errors))
		return fmt.Errorf("ConnectResponse contained the follwoing errors from the receiver: %v", connectResponse.Errors)
	}

	if connectResponse.Accepted {
		return nil
	} else {
		return &connection.ConnectRejectError{ConnID: connectResponse.ConnectionID, Reasons: connectResponse.RejectReasons}
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

		err := n.reputationStore.Read(p.ID(), func(rep reputation.Reputation) error {
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

func (n *Node) findWitness(conn connection.Connection, connOpts connection.Options, trptOpts transport.Options) (connection.Connection, error) {
	conn, err := conn.SetStatus(connection.RequestingWitness())
	if err != nil {
		return conn, err
	}

	witnessCandidates, err := n.witnessCandidates(conn)
	if err != nil {
		return conn, err
	}

	for _, p := range witnessCandidates {
		err := n.requestWitness(p, conn, connOpts, trptOpts)
		if err != nil {
			n.log.Error("Encountered error while requesting peer %s as witness for connection %s: %s", p.ID(), conn.ID(), err)
			continue
		}
		return conn.SetWitness(p.ID())
	}

	return conn, fmt.Errorf("Failed to find witness for connection %s", conn.ID())
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
	closeRequest := connection.CloseRequest{ConnectionID: conn.ID()}
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

	if witnessResponse.ConnectionID != conn.ID() {
		return fmt.Errorf("WitnessResponse contained the incorrect connection ID. Got: %s, Expected: %s", witnessResponse.ConnectionID, conn.ID())
	}

	if witnessResponse.Errors != nil && len(witnessResponse.Errors) > 0 {
		return fmt.Errorf("Encountered the following errors while requesting witness: %v", witnessResponse.Errors)
	}

	if witnessResponse.Accepted {
		return nil
	} else {
		return &connection.WitnessRejectError{ConnID: witnessResponse.ConnectionID, Reasons: witnessResponse.RejectReasons}
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
			ConnectionID:  request.ConnID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Found sender in peerstore: %s", sender)

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Found receiver in peerstore: %s", receiver)

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}
	h.node.log.Trace("ConnectRequest is from a node that is either going to be the sender or the receiver, so is permitted.")

	self, err := h.node.peerStore.Self()
	if err != nil {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}
	h.node.log.Trace("Got self: %s", self)

	if sender.ID() != self.ID() && receiver.ID() != self.ID() {
		return connection.ConnectResponse{
			ConnectionID:  request.ConnID,
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
	h.node.log.Debug("Preparing to accept connection %s into peerstore", request.ConnID)
	if err := h.node.connectionStore.Accept(request.ConnID, sender, receiver, connection.AwaitingWitness()); err != nil {
		if resp.Errors == nil {
			resp.Errors = []string{}
		}
		resp.Errors = append(resp.Errors, err.Error())
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
			ConnectionID:  request.ConnID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	receiver, err := h.node.peerStore.Find(request.Receiver)
	if err != nil {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnID,
			Errors:        []string{err.Error()},
			Accepted:      false,
			RejectReasons: []string{},
		}, nil
	}

	if sender.ID() != peerID && receiver.ID() != peerID {
		return connection.WitnessResponse{
			ConnectionID:  request.ConnID,
			Errors:        []string{},
			Accepted:      false,
			RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
		}, nil
	}

	resp, err := h.userHandler.Handle(peerID, request)
	if err != nil || len(resp.Errors) > 0 || !resp.Accepted {
		return resp, err
	}

	if err := h.node.connectionStore.Accept(request.ConnID, sender, receiver, connection.Open()); err != nil {
		if resp.Errors == nil {
			resp.Errors = []string{}
		}
		resp.Errors = append(resp.Errors, err.Error())
		return resp, nil
	}

	h.node.connectionStore.Update(request.ConnID, func(conn connection.Connection) (connection.Connection, error) {
		self, err := h.node.Self()
		if err != nil {
			return nil, err
		}
		return conn.SetWitness(self.ID())
	})

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

		conn, err = conn.SetWitness(notification.Witness)
		if err != nil {
			return nil, err
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
