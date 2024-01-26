package v2_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/peer"
)

func TestConnectHandler(t *testing.T) {
	ctx := createNodeContext(t)

	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to generate connection ID for tests: %s", err)
	}
	tests := []struct {
		peers []peer.Peer
		input struct {
			peerID peer.ID
			req    connection.ConnectRequest
		}
		handlerResp  *connectHandlerResp
		expectedResp connection.ConnectResponse
		expectedErr  error
		expectedConn *testConnection
	}{
		// failure: sender not in peer store
		{
			peers: []peer.Peer{},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"),
				connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{"No peer: sender"},
				Accepted:      false,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: receiver not in peer store
		{
			peers: []peer.Peer{},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("receiver"),
				connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("self"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{"No peer: receiver"},
				Accepted:      false,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: peerID not sender or receiver
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("self"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: self not sender or receiver
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"Connection is not to or from this node."},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: handler returns reject
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &connectHandlerResp{
				peerID: simpleID("sender"),
				request: connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.ConnectResponse{
					ConnectionID:  connID,
					Errors:        []string{},
					Accepted:      false,
					RejectReasons: []string{"test reject reason"},
				},
				err: nil,
			},
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"test reject reason"},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failrue: handler returns error
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &connectHandlerResp{
				peerID: simpleID("sender"),
				request: connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.ConnectResponse{},
				err:  errors.New("Test handler error"),
			},
			expectedResp: connection.ConnectResponse{},
			expectedErr:  errors.New("Test handler error"),
			expectedConn: nil,
		},
		// success: self is sender
		{
			peers: []peer.Peer{&testPeer{simpleID("receiver"), []peer.Address{"a1"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("receiver"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("self"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &connectHandlerResp{
				peerID: simpleID("receiver"),
				request: connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("self"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.ConnectResponse{
					ConnectionID:  connID,
					Errors:        []string{},
					Accepted:      true,
					RejectReasons: []string{},
				},
				err: nil,
			},
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      true,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: &testConnection{connID, simpleID("self"), nil, simpleID("receiver"), connection.AwaitingWitness()},
		},
		// success: self is receiver
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}},
			input: struct {
				peerID peer.ID
				req    connection.ConnectRequest
			}{
				simpleID("sender"), connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				},
			},
			handlerResp: &connectHandlerResp{
				peerID: simpleID("sender"),
				request: connection.ConnectRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				},
				resp: connection.ConnectResponse{
					ConnectionID:  connID,
					Errors:        []string{},
					Accepted:      true,
					RejectReasons: []string{},
				},
				err: nil,
			},
			expectedResp: connection.ConnectResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      true,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: &testConnection{connID, simpleID("sender"), nil, simpleID("self"), connection.AwaitingWitness()},
		},
	}

	for i, test := range tests {
		// setup
		for _, p := range test.peers {
			for _, a := range p.Addresses() {
				ctx.peerstore.AddAddr(p.ID(), a)
			}
		}
		if test.handlerResp != nil {
			ctx.connectHandler.resps = append(ctx.connectHandler.resps, *test.handlerResp)
		}

		// run test
		// use the transport connect handler as that's the wrapped one
		resp, err := ctx.transport.connectHandler.Handle(test.input.peerID, test.input.req)
		if !reflect.DeepEqual(test.expectedResp, resp) {
			t.Errorf("Test %d got incorrect resp. Expected: %v, Got: %v", i, test.expectedResp, resp)
			goto cleanup
		}
		if !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d got incorrect err. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		// Only check if conns match if it hasn't failed yet. If it failed already no point in checking.
		if conn := ctx.connectionStore.conns[test.input.req.ConnectionID]; !connsMatch(test.expectedConn, conn) {
			t.Errorf("Test %d incorrect connection. Expected: %s, Got: %s", i, test.expectedConn, conn)
		}

	cleanup:
		ctx.connectionStore.conns = map[connection.ID]*testConnection{}
		for _, p := range test.peers {
			delete(ctx.peerstore.peers, p.ID())
		}
		ctx.connectHandler.resps = nil
	}
}

func TestWitnessHandler(t *testing.T) {
	ctx := createNodeContext(t)

	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to generate connection ID for tests: %s", err)
	}
	tests := []struct {
		peers []peer.Peer
		input struct {
			peerID peer.ID
			req    connection.WitnessRequest
		}
		handlerResp  *witnessHandlerResp
		expectedResp connection.WitnessResponse
		expectedErr  error
		expectedConn *testConnection
	}{
		// failure: sender not in peer store
		{
			peers: []peer.Peer{},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("sender"),
				connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{"No peer: sender"},
				Accepted:      false,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: receiver not in peer store
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("receiver"),
				connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{"No peer: receiver"},
				Accepted:      false,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: peerID not sender or receiver
		{
			peers: []peer.Peer{
				&testPeer{simpleID("other"), []peer.Address{"a3"}},
				&testPeer{simpleID("sender"), []peer.Address{"a1"}},
				&testPeer{simpleID("receiver"), []peer.Address{"a2"}},
			},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("other"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"Will not accept connection from node that is not either the sender or the receiver."},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: self is sender
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("receiver"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("self"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"Cannot witness request in self is sender or receiver in."},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: self is receiver
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("sender"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("self"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: nil,
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"Cannot witness request in self is sender or receiver in."},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failure: handler returns reject
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("sender"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &witnessHandlerResp{
				peerID: simpleID("sender"),
				request: connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.WitnessResponse{
					ConnectionID:  connID,
					Errors:        []string{},
					Accepted:      false,
					RejectReasons: []string{"test reject reason"},
				},
				err: nil,
			},
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      false,
				RejectReasons: []string{"test reject reason"},
			},
			expectedErr:  nil,
			expectedConn: nil,
		},
		// failrue: handler returns error
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("sender"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &witnessHandlerResp{
				peerID: simpleID("sender"),
				request: connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.WitnessResponse{},
				err:  errors.New("Test handler error"),
			},
			expectedResp: connection.WitnessResponse{},
			expectedErr:  errors.New("Test handler error"),
			expectedConn: nil,
		},
		// success
		{
			peers: []peer.Peer{&testPeer{simpleID("sender"), []peer.Address{"a1"}}, &testPeer{simpleID("receiver"), []peer.Address{"a2"}}},
			input: struct {
				peerID peer.ID
				req    connection.WitnessRequest
			}{
				simpleID("receiver"), connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
			},
			handlerResp: &witnessHandlerResp{
				peerID: simpleID("receiver"),
				request: connection.WitnessRequest{
					ConnectionID: connID,
					Sender:       simpleID("sender"),
					Receiver:     simpleID("receiver"),
					Options:      connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				},
				resp: connection.WitnessResponse{
					ConnectionID:  connID,
					Errors:        []string{},
					Accepted:      true,
					RejectReasons: []string{},
				},
				err: nil,
			},
			expectedResp: connection.WitnessResponse{
				ConnectionID:  connID,
				Errors:        []string{},
				Accepted:      true,
				RejectReasons: []string{},
			},
			expectedErr:  nil,
			expectedConn: &testConnection{connID, simpleID("sender"), simpleID("self"), simpleID("receiver"), connection.Open()},
		},
	}

	for i, test := range tests {
		// setup
		for _, p := range test.peers {
			for _, a := range p.Addresses() {
				ctx.peerstore.AddAddr(p.ID(), a)
			}
		}
		if test.handlerResp != nil {
			ctx.witnessHandler.resps = append(ctx.witnessHandler.resps, *test.handlerResp)
		}

		// run test
		// use the transport connect handler as that's the wrapped one
		resp, err := ctx.transport.witnessHandler.Handle(test.input.peerID, test.input.req)
		if !reflect.DeepEqual(test.expectedResp, resp) {
			t.Errorf("Test %d got incorrect resp. Expected: %v, Got: %v", i, test.expectedResp, resp)
			goto cleanup
		}
		if !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d got incorrect err. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		// Only check if conns match if it hasn't failed yet. If it failed already no point in checking.
		if conn := ctx.connectionStore.conns[test.input.req.ConnectionID]; !connsMatch(test.expectedConn, conn) {
			t.Errorf("Test %d incorrect connection. Expected: %s, Got: %s", i, test.expectedConn, conn)
		}

	cleanup:
		ctx.connectionStore.conns = map[connection.ID]*testConnection{}
		for _, p := range test.peers {
			delete(ctx.peerstore.peers, p.ID())
		}
		ctx.witnessHandler.resps = nil
	}
}

func TestWitnessNotificationHandler(t *testing.T) {
	ctx := createNodeContext(t)

	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}

	expected := testPeer{simpleID("good"), []peer.Address{}}
	ctx.peerstore.peers[expected.id] = &expected

	witnessID := simpleID("witness")

	other := testPeer{simpleID("other"), []peer.Address{}}
	ctx.peerstore.peers[other.id] = &other

	tests := []struct {
		conn               *testConnection
		peerID             peer.ID
		req                connection.WitnessNotification
		handlerAction      *witnessNotificationHandlerResp
		expectedErr        error
		expectedConnStatus connection.Status
	}{
		// failure: no conn
		{
			conn:               nil,
			peerID:             expected.id,
			req:                connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction:      nil,
			expectedErr:        fmt.Errorf("No conn %s", connID),
			expectedConnStatus: nil,
		},
		// failure: wrong status
		{
			conn:          &testConnection{connID, nil, nil, nil, connection.NotifyingOfWitness()},
			peerID:        expected.id,
			req:           connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction: nil,
			expectedErr: fmt.Errorf(
				"Received witness notification from peer %s for connection %s that is not awaiting witness notification.",
				expected.id,
				connID,
			),
			expectedConnStatus: nil,
		},
		// failure: peerID is not expected peer, self sender
		{
			conn:          &testConnection{connID, expected.id, nil, ctx.peerstore.self.id, connection.AwaitingWitness()},
			peerID:        other.id,
			req:           connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction: nil,
			expectedErr: fmt.Errorf(
				"Received witness notification from unexpected peer for connection %s. Expected: %s, Got: %s",
				connID,
				expected.id,
				other.id,
			),
			expectedConnStatus: nil,
		},
		// failure: peerID is not expected peer, self receiver
		{
			conn:          &testConnection{connID, ctx.peerstore.self.id, nil, expected.id, connection.AwaitingWitness()},
			peerID:        other.id,
			req:           connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction: nil,
			expectedErr: fmt.Errorf(
				"Received witness notification from unexpected peer for connection %s. Expected: %s, Got: %s",
				connID,
				expected.id,
				other.id,
			),
			expectedConnStatus: nil,
		},
		// failure: persists wrapped handler error
		{
			conn:   &testConnection{connID, ctx.peerstore.self.id, nil, expected.id, connection.AwaitingWitness()},
			peerID: expected.id,
			req:    connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction: &witnessNotificationHandlerResp{
				peerID:  expected.id,
				request: connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
				err:     errors.New("Test handler error"),
			},
			expectedErr:        errors.New("Test handler error"),
			expectedConnStatus: nil,
		},
		// success
		{
			conn:   &testConnection{connID, ctx.peerstore.self.id, nil, expected.id, connection.AwaitingWitness()},
			peerID: expected.id,
			req:    connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
			handlerAction: &witnessNotificationHandlerResp{
				peerID:  expected.id,
				request: connection.WitnessNotification{ConnectionID: connID, Witness: witnessID},
				err:     nil,
			},
			expectedErr:        nil,
			expectedConnStatus: connection.Open(),
		},
	}

	for i, test := range tests {
		// setup
		if test.conn != nil {
			ctx.connectionStore.conns[test.conn.id] = test.conn
		}
		if test.handlerAction != nil {
			ctx.witnessNotificationHandler.resps = append(ctx.witnessNotificationHandler.resps, *test.handlerAction)
		}

		// run the test
		if err := ctx.transport.witnessNotificationHandler.Handle(test.peerID, test.req); !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d incorrect error. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		if conn := ctx.connectionStore.conns[test.req.ConnectionID]; test.expectedConnStatus != nil && conn.Status() != test.expectedConnStatus {
			t.Errorf("Test %d incorrect connection status. Expected: %s, Got: %s", i, test.expectedConnStatus, conn.Status())
		}

	cleanup:
		ctx.connectionStore.conns = map[connection.ID]*testConnection{}
		ctx.witnessNotificationHandler.resps = nil
	}
}

func TestCloseHandler(t *testing.T) {
	ctx := createNodeContext(t)

	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}

	peerID := simpleID("peer")

	tests := []struct {
		conn               *testConnection
		peerID             peer.ID
		req                connection.CloseRequest
		handlerAction      *closeHandlerResp
		expectedErr        error
		expectedConnStatus connection.Status
	}{
		// failure: no conn
		{
			conn:               nil,
			peerID:             peerID,
			req:                connection.CloseRequest{ConnectionID: connID},
			handlerAction:      nil,
			expectedErr:        fmt.Errorf("No conn %s", connID),
			expectedConnStatus: nil,
		},
		// failure: persists handler error
		{
			conn:   &testConnection{connID, nil, nil, nil, connection.Open()},
			peerID: peerID,
			req:    connection.CloseRequest{ConnectionID: connID},
			handlerAction: &closeHandlerResp{
				peerID:  peerID,
				request: connection.CloseRequest{ConnectionID: connID},
				err:     errors.New("Test handler error"),
			},
			expectedErr:        errors.New("Test handler error"),
			expectedConnStatus: nil,
		},
		// success: conn in status other than closed
		{
			conn:   &testConnection{connID, nil, nil, nil, connection.Open()},
			peerID: peerID,
			req:    connection.CloseRequest{ConnectionID: connID},
			handlerAction: &closeHandlerResp{
				peerID:  peerID,
				request: connection.CloseRequest{ConnectionID: connID},
				err:     nil,
			},
			expectedErr:        nil,
			expectedConnStatus: connection.Closed(),
		},
		// success: conn in closed status
		{
			conn:   &testConnection{connID, nil, nil, nil, connection.Closed()},
			peerID: peerID,
			req:    connection.CloseRequest{ConnectionID: connID},
			handlerAction: &closeHandlerResp{
				peerID:  peerID,
				request: connection.CloseRequest{ConnectionID: connID},
				err:     nil,
			},
			expectedErr:        nil,
			expectedConnStatus: connection.Closed(),
		},
	}

	for i, test := range tests {
		// setup
		if test.conn != nil {
			ctx.connectionStore.conns[test.conn.id] = test.conn
		}
		if test.handlerAction != nil {
			ctx.closeHandler.resps = append(ctx.closeHandler.resps, *test.handlerAction)
		}

		// run the test
		if err := ctx.transport.closeHandler.Handle(test.peerID, test.req); !isExpected(test.expectedErr, err) {
			t.Errorf("Test %d incorrect error. Expected: %s, Got: %s", i, test.expectedErr, err)
			goto cleanup
		}

		if conn := ctx.connectionStore.conns[test.req.ConnectionID]; test.expectedConnStatus != nil && conn.Status() != test.expectedConnStatus {
			t.Errorf("Test %d incorrect connection status. Expected: %s, Got: %s", i, test.expectedConnStatus, conn.Status())
		}

	cleanup:
		ctx.connectionStore.conns = map[connection.ID]*testConnection{}
		ctx.closeHandler.resps = nil
	}
}

// Need to use *testConnection rather than connection.Connectioon beceause of Go's typed nil
// https://codefibershq.com/blog/golang-why-nil-is-not-always-nil
func connsMatch(expected, actual *testConnection) bool {
	if expected == actual {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}
	return expected.ID() == actual.ID() &&
		expected.Sender() == actual.Sender() &&
		expected.Witness() == actual.Witness() &&
		expected.Receiver() == actual.Receiver() &&
		expected.Status() == actual.Status()
}
