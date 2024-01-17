package libp2p_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/arobie1992/go-clarinet/starterimpl/libp2p"
	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/transport"
	lp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/pkg/errors"
)

type logEntry struct {
	time    time.Time
	level   log.Level
	message string
}

type outputCapturingLog struct {
	level   log.Level
	entries []logEntry
}

func (l *outputCapturingLog) addEntry(level log.Level, fmtMsg string, args ...any) {
	l.entries = append(l.entries, logEntry{time.Now(), level, fmt.Sprintf(fmtMsg, args...)})
}

func (l *outputCapturingLog) Level() log.Level { return l.level }
func (l *outputCapturingLog) Debug(fmtMsg string, values ...any) {
	l.addEntry(log.Trace(), fmtMsg, values...)
}
func (l *outputCapturingLog) Error(fmtMsg string, values ...any) {
	l.addEntry(log.Debug(), fmtMsg, values...)
}
func (l *outputCapturingLog) Info(fmtMsg string, values ...any) {
	l.addEntry(log.Info(), fmtMsg, values...)
}
func (l *outputCapturingLog) Trace(fmtMsg string, values ...any) {
	l.addEntry(log.Warn(), fmtMsg, values...)
}
func (l *outputCapturingLog) Warn(fmtMsg string, values ...any) {
	l.addEntry(log.Error(), fmtMsg, values...)
}

// not currently used, but helpful for debugging failures
func writeLogs(t *testing.T, ctx transportCtx) {
	t.Helper()
	senderLogs := []string{}
	for _, e := range ctx.senderLog.entries {
		senderLogs = append(senderLogs, "\""+e.message+"\",")
	}
	t.Logf("Sender log: %v", senderLogs)
	receiverLogs := []string{}
	for _, e := range ctx.receiverLog.entries {
		receiverLogs = append(receiverLogs, "\""+e.message+"\",")
	}
	t.Logf("Receiver log: %v", receiverLogs)
}

type transportCtx struct {
	sender      host.Host
	senderTx    transport.Transport
	senderLog   *outputCapturingLog
	receiver    host.Host
	receiverTx  transport.Transport
	receiverLog *outputCapturingLog
}

type simpleID string

func (id simpleID) String() string {
	return string(id)
}

func createTransportCtx() (*transportCtx, error) {
	// port 0 should tell it to pick a random ephemeral port
	sender, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		return nil, err
	}
	receiver, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		return nil, err
	}
	for _, addr := range receiver.Addrs() {
		sender.Peerstore().AddAddr(receiver.ID(), addr, peerstore.PermanentAddrTTL)
	}
	for _, addr := range sender.Addrs() {
		receiver.Peerstore().AddAddr(receiver.ID(), addr, peerstore.PermanentAddrTTL)
	}

	senderLog := outputCapturingLog{log.Info(), []logEntry{}}
	senderTx := libp2p.NewTransport(sender, &senderLog)

	receiverLog := outputCapturingLog{log.Info(), []logEntry{}}
	receiverTx := libp2p.NewTransport(receiver, &receiverLog)
	return &transportCtx{sender, senderTx, &senderLog, receiver, receiverTx, &receiverLog}, nil
}

type simplePeer struct {
	id        peer.ID
	addresses []peer.Address
}

func (p *simplePeer) Addresses() []peer.Address {
	return p.addresses
}

func (p *simplePeer) ID() peer.ID {
	return p.id
}

func TestTransportSend(t *testing.T) {
	ctx, err := createTransportCtx()
	if err != nil {
		t.Fatalf("Error while creating ctx: %s", err)
	}
	ctx.receiverTx.RegisterHandlers(
		&testConnectHandler{connection.ConnectResponse{}, nil, transport.Options{}},
		&testWitnessHandler{connection.WitnessResponse{}, nil, transport.Options{}},
		&testWitnessNotificationHandler{nil, transport.Options{}},
		&testCloseHandler{nil, transport.Options{}},
	)

	receiverPeer := &simplePeer{ctx.receiver.ID(), []peer.Address{}}
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Error while creating connecction ID: %s", err)
	}

	tests := []any{
		connection.CloseRequest{ConnectionID: connID},
		connection.WitnessNotification{ConnectionID: connID, Witness: simpleID("witness")},
	}

	for i, test := range tests {
		if err := ctx.senderTx.Send(receiverPeer, transport.Options{}, test); err != nil {
			t.Errorf("Test %d got error while sending: %s", i, err)
		}
	}
}

func TestTransportExchange(t *testing.T) {
	ctx, err := createTransportCtx()
	if err != nil {
		t.Fatalf("Error while creating ctx: %s", err)
	}

	receiverPeer := &simplePeer{ctx.receiver.ID(), []peer.Address{}}
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Error while creating connecction ID: %s", err)
	}

	ctx.receiverTx.RegisterHandlers(
		&testConnectHandler{connection.ConnectResponse{
			ConnectionID:  connID,
			Errors:        []string{"test connect error resp"},
			Accepted:      true,
			RejectReasons: []string{"test connect reject reason"},
		}, nil, transport.Options{}},
		&testWitnessHandler{connection.WitnessResponse{
			ConnectionID:  connID,
			Errors:        []string{"test witness error resp"},
			Accepted:      true,
			RejectReasons: []string{"test witness reject reason"},
		}, nil, transport.Options{}},
		&testWitnessNotificationHandler{nil, transport.Options{}},
		&testCloseHandler{nil, transport.Options{}},
	)

	tests := []struct {
		req          any
		resp         any
		expectedResp any
	}{
		{
			connection.ConnectRequest{
				ConnID:   connID,
				Sender:   ctx.sender.ID(),
				Receiver: ctx.receiver.ID(),
				Options:  connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
			},
			&connection.ConnectResponse{},
			// test all the fields at once
			&connection.ConnectResponse{
				ConnectionID: connID,
				Errors:       []string{"test connect error resp"},
				// use true because booleans should default to false
				Accepted:      true,
				RejectReasons: []string{"test connect reject reason"},
			},
		},
		{
			connection.WitnessRequest{
				ConnID:   connID,
				Sender:   ctx.sender.ID(),
				Receiver: ctx.receiver.ID(),
				Options:  connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
			},
			&connection.WitnessResponse{},
			// test all the fields at once
			&connection.WitnessResponse{
				ConnectionID: connID,
				Errors:       []string{"test witness error resp"},
				// use true because booleans should default to false
				Accepted:      true,
				RejectReasons: []string{"test witness reject reason"},
			},
		},
	}

	for i, test := range tests {
		if _, err := ctx.senderTx.Exchange(receiverPeer, transport.Options{}, test.req, test.resp); err != nil {
			t.Errorf("Test %d got error while sending: %s", i, err)
		}
		if !reflect.DeepEqual(test.resp, test.expectedResp) {
			t.Errorf("Test %d response did not match. Expected: %v, Got: %v", i, test.expectedResp, test.resp)
		}
	}
}

func TestIncorrectTypeError(t *testing.T) {
	ctx, err := createTransportCtx()
	if err != nil {
		t.Fatalf("Error while creating ctx: %s", err)
	}
	ctx.receiverTx.RegisterHandlers(
		&testConnectHandler{connection.ConnectResponse{}, nil, transport.Options{}},
		&testWitnessHandler{connection.WitnessResponse{}, nil, transport.Options{}},
		&testWitnessNotificationHandler{nil, transport.Options{}},
		&testCloseHandler{nil, transport.Options{}},
	)

	receiverPeer := &simplePeer{ctx.receiver.ID(), []peer.Address{}}
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Error while creating connecction ID: %s", err)
	}

	tests := []struct {
		fn          func() error
		expectedErr error
	}{
		{
			func() error {
				return ctx.senderTx.Send(receiverPeer, transport.Options{}, "test")
			},
			errors.New("Unsupported data type: string. Supported types are: [connection.ConnectRequest, connection.WitnessRequest, connection.CloseRequest, connection.WitnessNotification]"),
		},
		{
			func() error {
				_, err := ctx.senderTx.Exchange(receiverPeer, transport.Options{}, "test", map[string]string{})
				return err
			},
			errors.New("Unsupported data type: string. Supported types are: [connection.ConnectRequest, connection.WitnessRequest, connection.CloseRequest, connection.WitnessNotification]"),
		},
		{
			func() error {
				req := connection.ConnectRequest{
					ConnID:   connID,
					Sender:   ctx.sender.ID(),
					Receiver: ctx.receiver.ID(),
					Options:  connection.Options{WitnessSelector: connection.WitnessSelectorSender()},
				}
				_, err := ctx.senderTx.Exchange(receiverPeer, transport.Options{}, req, map[string]string{})
				return err
			},
			fmt.Errorf("Failed to decode response: Cannot deserialize unrecognized type: %T", map[string]string{}),
		},
	}

	for i, test := range tests {
		err := test.fn()
		if err == nil || err.Error() != test.expectedErr.Error() {
			t.Errorf("Test %d error did not match. Expected: %s, Got: %s", i, test.expectedErr, err)
		}
	}
}

func TestReceiverReset(t *testing.T) {
	ctx, err := createTransportCtx()
	if err != nil {
		t.Fatalf("Error while creating ctx: %s", err)
	}
	ctx.receiverTx.RegisterHandlers(
		&testConnectHandler{connection.ConnectResponse{}, errors.New("test connect error"), transport.Options{}},
		&testWitnessHandler{connection.WitnessResponse{}, nil, transport.Options{}},
		&testWitnessNotificationHandler{nil, transport.Options{}},
		&testCloseHandler{errors.New("test close error"), transport.Options{}},
	)

	receiverPeer := &simplePeer{ctx.receiver.ID(), []peer.Address{}}
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Error while creating connecction ID: %s", err)
	}

	tests := []struct {
		fn                func() error
		expectedSenderErr error
	}{
		{
			func() error {
				req := connection.ConnectRequest{
					ConnID:   connID,
					Sender:   ctx.sender.ID(),
					Receiver: ctx.receiver.ID(),
					Options:  connection.Options{WitnessSelector: connection.WitnessSelectorReceiver()},
				}
				_, err := ctx.senderTx.Exchange(receiverPeer, transport.Options{}, req, &connection.ConnectResponse{})
				return err
			},
			errors.New("stream reset"),
		},
		{
			func() error {
				req := connection.CloseRequest{ConnectionID: connID}
				return ctx.senderTx.Send(receiverPeer, transport.Options{}, req)
			},
			nil,
		},
	}

	for i, test := range tests {
		err := test.fn()
		if test.expectedSenderErr == nil && err == nil {
			continue
		}
		if err == nil || err.Error() != test.expectedSenderErr.Error() {
			t.Errorf("Test %d did not get sender side error. Expected: %s, Got: %s", i, test.expectedSenderErr, err)
		}
	}
}

// TODO:
// - errors in response for connect
// - reject & reasons for connect
// - errors in response for witness
// - reject & reasons for witness

//
// These are the basic handlers that can be used for any test that requires success.
//

type testConnectHandler struct {
	resp    connection.ConnectResponse
	err     error
	options transport.Options
}

func (h *testConnectHandler) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	return h.resp, h.err
}

func (h *testConnectHandler) Options() transport.Options {
	return h.options
}

type testWitnessHandler struct {
	resp    connection.WitnessResponse
	err     error
	options transport.Options
}

func (h *testWitnessHandler) Handle(peerID peer.ID, request connection.WitnessRequest) (connection.WitnessResponse, error) {
	return h.resp, h.err
}

func (h *testWitnessHandler) Options() transport.Options {
	return h.options
}

type testWitnessNotificationHandler struct {
	err     error
	options transport.Options
}

func (h *testWitnessNotificationHandler) Handle(peerID peer.ID, notification connection.WitnessNotification) error {
	return h.err
}

func (h *testWitnessNotificationHandler) Options() transport.Options {
	return h.options
}

type testCloseHandler struct {
	err     error
	options transport.Options
}

func (h *testCloseHandler) Handle(peerID peer.ID, request connection.CloseRequest) error {
	return h.err
}

func (h *testCloseHandler) Options() transport.Options {
	return h.options
}

//
// peerstore tests
//

type peerstoreCtx struct {
	host      host.Host
	peerstore peer.PeerStore
	l         *outputCapturingLog
}

func createPeerstoreCtx() (*peerstoreCtx, error) {
	h, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		return nil, err
	}
	l := outputCapturingLog{log.Info(), []logEntry{}}
	peerstore := libp2p.NewPeerStore(h, &l)
	return &peerstoreCtx{h, peerstore, &l}, nil
}

func TestPeerStoreAddAddrAndFind(t *testing.T) {
	ctx, err := createPeerstoreCtx()
	if err != nil {
		t.Fatalf("Failed to create peerstore: %s", err)
	}

	p1, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		t.Fatalf("Failed to create p1: %s", err)
	}
	p2, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		t.Fatalf("Failed to create p2: %s", err)
	}

	tests := []struct {
		peerID      peer.ID
		addr        peer.Address
		expectedErr error
	}{
		{p1.ID(), peer.Address(p1.Addrs()[0].String()), nil},
		{p2.ID(), "bad mutliaddr", errors.New(`failed to parse multiaddr "bad mutliaddr": must begin with /`)},
		{
			simpleID("bad peerID"),
			peer.Address(p2.Addrs()[0].String()),
			errors.New(`failed to parse peer ID: invalid cid: illegal base32 data at input byte 2`),
		},
	}

	for i, test := range tests {
		err := ctx.peerstore.AddAddr(test.peerID, test.addr)
		if err == nil && test.expectedErr == nil {
			p, err := ctx.peerstore.Find(test.peerID)
			if err != nil {
				t.Errorf("Test %d encountered error while finding peer: %s", i, err)
			}
			if test.peerID != p.ID() {
				t.Errorf("Test %d peer ID did not match. Expected: %s, Got: %s", i, test.peerID, p.ID())
			}
			addrs := []peer.Address{test.addr}
			if !reflect.DeepEqual(addrs, p.Addresses()) {
				t.Errorf("Test %d addresses did not match. Expected: %v, got: %v", i, addrs, p.Addresses())
			}
			continue
		}
		if err == nil || err.Error() != test.expectedErr.Error() {
			t.Errorf("Test %d incorrect error response. Expected: %s, Got; %s", i, test.expectedErr, err)
		}
	}
}

func TestPeerStoreSelf(t *testing.T) {
	ctx, err := createPeerstoreCtx()
	if err != nil {
		t.Fatalf("Failed to create peerstore context: %s", err)
	}

	self, err := ctx.peerstore.Self()
	if err != nil {
		t.Fatalf("Failed to find self: %s", err)
	}

	if self.ID() != ctx.host.ID() {
		t.Errorf("Incorrect self ID. Expected: %s, Got: %s", ctx.host.ID(), self.ID())
	}

	// right now there's only one address; this will need updating if more are added
	if len(ctx.host.Addrs()) != 1 {
		t.Fatalf("Someone changed the number of addresses so this test needs updating.")
	}
	hostAddrs := []peer.Address{peer.Address(ctx.host.Addrs()[0].String())}
	if !reflect.DeepEqual(hostAddrs, self.Addresses()) {
		t.Fatalf("Self addrs did not match. Expected: %v, Got: %v", hostAddrs, self.Addresses())
	}
}

func TestPeerStoreAddAddrAll(t *testing.T) {
	ctx, err := createPeerstoreCtx()
	if err != nil {
		t.Fatalf("Failed to create peerstore: %s", err)
	}

	p, err := lp2p.New(lp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"), lp2p.DisableRelay())
	if err != nil {
		t.Fatalf("Failed to create p: %s", err)
	}

	ctx.peerstore.AddAddr(p.ID(), peer.Address(p.Addrs()[0].String()))

	expectedSelf := libp2p.NewPeer(ctx.host.ID(), []peer.Address{peer.Address(ctx.host.Addrs()[0].String())})
	expectedP := libp2p.NewPeer(p.ID(), []peer.Address{peer.Address(p.Addrs()[0].String())})

	all, err := ctx.peerstore.All()
	if err != nil {
		t.Fatalf("Encountered error while calling all: %s", err)
	}

	if len(all) != 2 {
		t.Fatalf("Incorrect number of peers in all list. Expected: 2, Got: %d", len(all))
	}

	foundSelf := false
	foundP := false

	for _, ap := range all {
		if reflect.DeepEqual(expectedSelf, ap) {
			foundSelf = true
			continue
		}
		if reflect.DeepEqual(expectedP, ap) {
			foundP = true
		}
	}

	if !foundSelf {
		t.Error("All did not contain self.")
	}
	if !foundP {
		t.Error("All did not contain p.")
	}
}
