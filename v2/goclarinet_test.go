package v2_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	v2 "github.com/arobie1992/go-clarinet/v2"
	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
	"github.com/arobie1992/go-clarinet/v2/transport"
)

type nodeContext struct {
	peerstore                  *testPeerStore
	connectionStore            *testConnectionStore
	messageStore               *testMessageStore
	reputationStore            *testReputationStore
	transport                  *testTransport
	connectHandler             *testConnectHandler
	witnessHandler             *testWitnessHandler
	witnessNotificationHandler *testWitnessNotificationHandler
	closeHandler               *testCloseHandler
	log                        *outputCapturingLog
	node                       *v2.Node
}

func createNodeContext(t *testing.T) nodeContext {
	t.Helper()
	ps := &testPeerStore{&testPeer{simpleID("self"), []peer.Address{"selfAddr"}}, map[peer.ID]*testPeer{}, map[any]any{}}
	ps.AddAddr(ps.self.id, ps.self.addresses[0])
	l := &outputCapturingLog{log.Info(), []logEntry{}}
	ctx := nodeContext{
		ps,
		&testConnectionStore{map[connection.ID]*testConnection{}},
		&testMessageStore{},
		&testReputationStore{map[peer.ID]*testRep{}},
		&testTransport{},
		&testConnectHandler{},
		&testWitnessHandler{},
		&testWitnessNotificationHandler{},
		&testCloseHandler{},
		l,
		nil,
	}
	node, err := v2.NewNode(
		ctx.peerstore,
		ctx.connectionStore,
		ctx.messageStore,
		ctx.reputationStore,
		func(rep reputation.Reputation) (bool, error) {
			return true, nil
		},
		ctx.transport,
		transport.Handlers{
			ConnectHandler:             ctx.connectHandler,
			WitnessHandler:             ctx.witnessHandler,
			WitnessNotificationHandler: ctx.witnessNotificationHandler,
			CloseHandler:               ctx.closeHandler,
		},
		l,
	)
	if err != nil {
		t.Fatalf("Failed to create nodeContext: %s", err)
	}
	ctx.node = node
	return ctx
}

func TestSelf(t *testing.T) {
	ctx := createNodeContext(t)
	tests := []struct {
		self peer.Peer
		err  error
	}{
		{&testPeer{simpleID("peer"), []peer.Address{}}, nil},
		{nil, errors.New("Failed to find self")},
	}

	for i, test := range tests {
		ctx.peerstore.actions["Self"] = test
		self, err := ctx.node.Self()
		if self != test.self || err != test.err {
			t.Errorf("Test %d incorrect output. Expected: %v, %s, Got: %v, %s", i, test.self, test.err, self, err)
		}
	}
}

func TestPeerOperations(t *testing.T) {
	ctx := createNodeContext(t)

	tests := []struct {
		peer     peer.Peer
		expected []peer.Peer
	}{
		{nil, []peer.Peer{ctx.peerstore.self}},
		{
			&testPeer{simpleID("peer1"), []peer.Address{"p1addr"}},
			[]peer.Peer{&testPeer{simpleID("peer1"), []peer.Address{"p1addr"}}, ctx.peerstore.self},
		},
		{
			&testPeer{simpleID("peer2"), []peer.Address{"p2addr"}},
			[]peer.Peer{
				&testPeer{simpleID("peer1"), []peer.Address{"p1addr"}},
				&testPeer{simpleID("peer2"), []peer.Address{"p2addr"}},
				ctx.peerstore.self,
			},
		},
		{
			&testPeer{simpleID("peer1"), []peer.Address{"p1addr", "p1addr2"}},
			[]peer.Peer{
				&testPeer{simpleID("peer1"), []peer.Address{"p1addr", "p1addr2"}},
				&testPeer{simpleID("peer2"), []peer.Address{"p2addr"}},
				ctx.peerstore.self,
			},
		},
		{
			&testPeer{simpleID("peer2"), []peer.Address{"p2addr2"}},
			[]peer.Peer{
				&testPeer{simpleID("peer1"), []peer.Address{"p1addr", "p1addr2"}},
				&testPeer{simpleID("peer2"), []peer.Address{"p2addr", "p2addr2"}},
				ctx.peerstore.self,
			},
		},
	}

	for i, test := range tests {
		if test.peer != nil {
			ctx.node.UpdatePeer(test.peer)
		}
		peers, err := ctx.node.Peers()
		if err != nil {
			t.Fatalf("Peers call failed: %s", err)
		}
		if len(peers) != len(test.expected) {
			t.Errorf("Test %d incorrect number of peers returned. Expected: %d, Got: %d", i, len(test.expected), len(peers))
		}
		// bool should default to false, and I'm lazy
		found := make([]bool, len(test.expected))
		for _, p := range peers {
			for j, ep := range test.expected {
				if reflect.DeepEqual(p, ep) {
					found[j] = true
					break
				}
			}
		}

		for j, f := range found {
			if !f {
				t.Errorf("Test %d missing expected peer %d: %v", i, j, test.expected[j])
			}
		}
		if t.Failed() {
			fmtpeers := []string{}
			for _, p := range peers {
				fmtpeers = append(fmtpeers, fmt.Sprintf("%v", p))
			}
			fmtexpected := []string{}
			for _, p := range test.expected {
				fmtexpected = append(fmtexpected, fmt.Sprintf("%v", p))
			}
			t.Logf("Expected: %v, Got: %v", fmtexpected, fmtpeers)
		}
	}

	ctx.peerstore.actions["AddAddr"] = struct{ err error }{errors.New("Failed to add addr")}
	if err := ctx.node.UpdatePeer(&testPeer{simpleID("fail"), []peer.Address{"fail"}}); err == nil || err.Error() != "Failed to add addr" {
		t.Errorf("UpdatePeer failed to propagate error. Expected: Failed to add addr, Got: %s", err)
	}
	delete(ctx.peerstore.actions, "AddAddr")

	ctx.peerstore.actions["All"] = struct {
		peers []peer.Peer
		err   error
	}{nil, errors.New("Failed to retrieve peers")}
	if peers, err := ctx.node.Peers(); peers != nil || err == nil || err.Error() != "Failed to retrieve peers" {
		t.Errorf("Peers failed to persist error. Expected: nil, Failed to retrieve peers, Got: %v, %s", peers, err)
	}
}

func TestConnect(t *testing.T) {
	type Inputs struct {
		receiver peer.Peer
		connOpts connection.Options
		txOpts   transport.Options
	}
	ctx := createNodeContext(t)

	mismatchedConnID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to generate connection ID for mismatched connection scenario: %s", err)
	}
	tests := []struct {
		inputs            Inputs
		witnessCandidates []peer.Peer
		witnessReps       []*testRep
		exchangeActions   []exchangeAction
		sendActions       []sendAction
		expectedConn      *testConnection
		expectedErr       error
		expectedErrFmt    string
	}{
		// success: sender selects witness
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{
				// test that it ignores self and receiver
				&testPeer{simpleID("self"), []peer.Address{}},
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				&testPeer{simpleID("wit1"), []peer.Address{"witaddr"}},
			},
			witnessReps: []*testRep{{simpleID("wit1"), 0, 0, 0, float64(1)}},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorSender()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{},
						Accepted:      true,
						RejectReasons: []string{},
					}
					return match, 50, nil
				},
				// witness request
				func(p peer.Peer, o transport.Options, data, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("wit1"), []peer.Address{"witaddr"}})
					match = match && reflect.DeepEqual(o, transport.Options{})
					typedData, ok := data.(connection.WitnessRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorSender()
					typedResp, ok := resp.(*connection.WitnessResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.WitnessResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{},
						Accepted:      true,
						RejectReasons: []string{},
					}
					return match, 50, nil
				},
			},
			sendActions: []sendAction{
				func(p peer.Peer, o transport.Options, data any) (bool, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(o, transport.Options{})
					typedData, ok := data.(connection.WitnessNotification)
					match = match && ok
					if !match {
						return match, nil
					}
					match = match && typedData.Witness == simpleID("wit1")
					return match, nil
				},
			},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, simpleID("wit1"), simpleID("recv1"), connection.Open()},
			expectedErr:    nil,
			expectedErrFmt: "",
		},
		// success: receiver selects witness
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{},
			witnessReps:       []*testRep{},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorReceiver()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{},
						Accepted:      true,
						RejectReasons: []string{},
					}
					return match, 50, nil
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.AwaitingWitness()},
			expectedErr:    nil,
			expectedErrFmt: "",
		},
		// failure: errors in connect response
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{},
			witnessReps:       []*testRep{},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorReceiver()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{"test connect error"},
						Accepted:      true,
						RejectReasons: []string{},
					}
					return match, 50, nil
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.RequestingReceiver()},
			expectedErr:    errors.New("ConnectResponse contained the follwoing errors from the receiver: [test connect error]"),
			expectedErrFmt: "",
		},
		// failure: rejected connect response
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{},
			witnessReps:       []*testRep{},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorReceiver()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{},
						Accepted:      false,
						RejectReasons: []string{"test reject reason"},
					}
					return match, 50, nil
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.RequestingReceiver()},
			expectedErr:    nil,
			expectedErrFmt: "Connection %s connect request was rejected for the following reasons: [test reject reason]",
		},
		// failure: exchange error
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{},
			witnessReps:       []*testRep{},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorReceiver()
					_, ok = resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					return match, 0, errors.New("Test connect request transport error")
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.RequestingReceiver()},
			expectedErr:    errors.New("Test connect request transport error"),
			expectedErrFmt: "",
		},
		// failure: mismatched conn id in connecct response
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{},
			witnessReps:       []*testRep{},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorReceiver()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  mismatchedConnID,
						Errors:        []string{},
						Accepted:      false,
						RejectReasons: []string{"test reject reason"},
					}
					return match, 50, nil
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.RequestingReceiver()},
			expectedErr:    nil,
			expectedErrFmt: "ConnectResponse contained the incorrect connection ID. Expected: %s, Got: " + mismatchedConnID.String(),
		},
		// failure: no trusted witnesses available
		{
			inputs: Inputs{
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				transport.Options{},
			},
			witnessCandidates: []peer.Peer{
				// test that it ignores self and receiver
				&testPeer{simpleID("self"), []peer.Address{}},
				&testPeer{simpleID("recv1"), []peer.Address{"addr1"}},
				&testPeer{simpleID("wit1"), []peer.Address{"witaddr"}},
			},
			witnessReps: []*testRep{{simpleID("wit1"), 0, 0, 0, float64(0.25)}},
			exchangeActions: []exchangeAction{
				// connect request
				func(p peer.Peer, opts transport.Options, data any, resp any) (bool, int, error) {
					match := reflect.DeepEqual(p, &testPeer{simpleID("recv1"), []peer.Address{"addr1"}})
					match = match && reflect.DeepEqual(opts, transport.Options{})
					typedData, ok := data.(connection.ConnectRequest)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					match = match && typedData.Sender == simpleID("self")
					match = match && typedData.Receiver == simpleID("recv1")
					match = match && typedData.Options.WitnessSelector == connection.WitnessSelectorSender()
					typedResp, ok := resp.(*connection.ConnectResponse)
					match = match && ok
					if !match {
						return match, 0, nil
					}
					*typedResp = connection.ConnectResponse{
						ConnectionID:  typedData.ConnectionID,
						Errors:        []string{},
						Accepted:      true,
						RejectReasons: []string{},
					}
					return match, 50, nil
				},
			},
			sendActions:    []sendAction{},
			expectedConn:   &testConnection{connection.ID{}, ctx.peerstore.self.id, nil, simpleID("recv1"), connection.RequestingWitness()},
			expectedErr:    nil,
			expectedErrFmt: "Failed to find witness for connection %s",
		},
	}

	for i, test := range tests {
		// setup
		for _, r := range test.witnessReps {
			ctx.reputationStore.reps[r.peerID] = r
		}
		ctx.peerstore.actions["All"] = struct {
			peers []peer.Peer
			err   error
		}{test.witnessCandidates, nil}
		for _, ea := range test.exchangeActions {
			ctx.transport.exchangeActions = append(ctx.transport.exchangeActions, ea)
		}
		for _, sa := range test.sendActions {
			ctx.transport.sendActions = append(ctx.transport.sendActions, sa)
		}

		// run the test
		connID, err := ctx.node.Connect(test.inputs.receiver, test.inputs.connOpts, test.inputs.txOpts)

		// Go currently requires all variables declared before gotos, maybe someday: https://github.com/golang/go/issues/26058
		// Use goto because we need to clean up, defer is function scoped rather than lexically scoped, and extracting this into a function
		// just to use defer or perform cleanup in the caller doesn't seem beneficial
		found := false
		var conns []connection.Connection

		if test.expectedErr == nil && test.expectedErrFmt != "" {
			test.expectedErr = fmt.Errorf(test.expectedErrFmt, connID)
		}
		if !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d incorrect error output. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		conns, err = ctx.node.Connections()
		if err != nil {
			t.Errorf("Test %d failed to retrieve connections: %s", i, err)
			continue
		}

		test.expectedConn.id = connID
		for _, c := range conns {
			match := c.ID() == test.expectedConn.ID()
			match = match && c.Sender() == test.expectedConn.Sender()
			match = match && c.Witness() == test.expectedConn.Witness()
			match = match && c.Receiver() == test.expectedConn.Receiver()
			match = match && c.Status() == test.expectedConn.Status()
			if match {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Test %d failed to find connection %s", i, test.expectedConn)
		}

	cleanup:
		ctx.reputationStore.reps = map[peer.ID]*testRep{}
		delete(ctx.peerstore.actions, "All")
		ctx.transport.exchangeActions = nil
		ctx.transport.sendActions = nil

		if t.Failed() {
			t.Logf("Test %d log entries: %v", i, formatLogEntries(ctx.log))
			t.Logf("Test %d conns: %v", i, formatConns(ctx.connectionStore.conns))
		}
	}
}

func TestClose(t *testing.T) {
	ctx := createNodeContext(t)

	witness := &testPeer{simpleID("witness"), []peer.Address{"witnessaddr"}}
	receiver := &testPeer{simpleID("receiver"), []peer.Address{"receiveraddr"}}

	ctx.peerstore.peers[witness.id] = witness
	ctx.peerstore.peers[receiver.id] = receiver

	tests := []struct {
		connection     *testConnection
		options        transport.Options
		sendActions    []func(connection.ID) sendAction
		expectedConn   *testConnection
		expectedErr    error
		expectedErrFmt string
	}{
		{nil, transport.Options{}, []func(connection.ID) sendAction{}, nil, nil, "No conn %s"},
		{
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.RequestingReceiver()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.RequestingSender()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.AwaitingWitness()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.RequestingWitness()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), nil, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.NotifyingOfWitness()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(witness, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Open()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(witness, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closing()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(witness, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closed()},
			transport.Options{},
			[]func(connection.ID) sendAction{},
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closed()},
			nil,
			"",
		},
		{
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Open()},
			transport.Options{},
			[]func(connection.ID) sendAction{
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(witness, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, errors.New("Test closing error")
					}
				},
				func(id connection.ID) sendAction {
					return func(p peer.Peer, o transport.Options, data any) (bool, error) {
						match := reflect.DeepEqual(receiver, p)
						match = match && reflect.DeepEqual(o, transport.Options{})
						typedData, ok := data.(connection.CloseRequest)
						match = match && ok
						if !match {
							return false, nil
						}
						match = match && typedData.ConnectionID == id
						return match, nil
					}
				},
			},
			&testConnection{connection.ID{}, simpleID("self"), witness.id, receiver.id, connection.Closing()},
			nil,
			"Errors encountered while closing connectin %s: [Encountered error sending close request to peer witness: Test closing error <nil>]",
		},
	}

	for i, test := range tests {
		// setup
		connID, err := connection.NewRandomID()
		if err != nil {
			t.Errorf("Test %d failed to create connection ID: %s", i, err)
		}
		if test.connection != nil {
			test.connection.id = connID
			ctx.connectionStore.conns[connID] = test.connection
			// expected conn should only be null if the initial conn is null
			test.expectedConn.id = connID
		}

		for _, ses := range test.sendActions {
			se := ses(connID)
			ctx.transport.sendActions = append(ctx.transport.sendActions, se)
		}

		if test.expectedErr == nil && test.expectedErrFmt != "" {
			test.expectedErr = fmt.Errorf(test.expectedErrFmt, connID)
		}

		// run the test

		// Go currently requires all variables declared before gotos, maybe someday: https://github.com/golang/go/issues/26058
		// Use goto because we need to clean up, defer is function scoped rather than lexically scoped, and extracting this into a function
		// just to use defer or perform cleanup in the caller doesn't seem beneficial
		found := false
		var conns []connection.Connection

		err = ctx.node.CloseConnection(connID, test.options)
		if !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d got incorrect error. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		conns, err = ctx.node.Connections()
		if err != nil {
			t.Errorf("Test %d failed to retrieve connections: %s", i, err)
			continue
		}

		if test.connection == nil {
			if len(conns) != 0 {
				t.Errorf("Test %d found conn when it shouldn't have.", i)
			}
			goto cleanup
		}

		for _, c := range conns {
			match := c.ID() == test.expectedConn.ID()
			match = match && c.Sender() == test.expectedConn.Sender()
			match = match && c.Witness() == test.expectedConn.Witness()
			match = match && c.Receiver() == test.expectedConn.Receiver()
			match = match && c.Status() == test.expectedConn.Status()
			if match {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Test %d failed to find connection %s", i, test.expectedConn)
		}

	cleanup:
		ctx.connectionStore.conns = map[connection.ID]*testConnection{}
		ctx.transport.sendActions = nil
	}
}

func isExpected(expected, actual error) bool {
	if expected == actual {
		return true
	}
	if expected == nil || actual == nil || expected.Error() != actual.Error() {
		return false
	}
	return true
}
