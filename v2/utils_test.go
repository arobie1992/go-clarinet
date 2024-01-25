package v2_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/data"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
	"github.com/arobie1992/go-clarinet/v2/transport"
)

type simpleID string

func (id simpleID) String() string {
	return string(id)
}

type testPeer struct {
	id        peer.ID
	addresses []peer.Address
}

func (p *testPeer) Addresses() []peer.Address {
	return p.addresses
}

func (p *testPeer) ID() peer.ID {
	return p.id
}

type testPeerStore struct {
	self    *testPeer
	peers   map[peer.ID]*testPeer
	actions map[any]any
}

func (ps *testPeerStore) AddAddr(id peer.ID, addr peer.Address) error {
	action, ok := ps.actions["AddAddr"]
	if ok {
		typed, ok := action.(struct{ err error })
		if !ok {
			return errors.New("AddAddr action must be of type struct {err error}")
		}
		return typed.err
	}
	if _, ok := ps.peers[id]; !ok {
		ps.peers[id] = &testPeer{id, []peer.Address{}}
	}
	if !slices.Contains(ps.peers[id].addresses, addr) {
		ps.peers[id].addresses = append(ps.peers[id].addresses, addr)
	}
	return nil
}

func (ps *testPeerStore) All() ([]peer.Peer, error) {
	action, ok := ps.actions["All"]
	if ok {
		typed, ok := action.(struct {
			peers []peer.Peer
			err   error
		})
		if !ok {
			return nil, errors.New("AddAddr action must be of type struct {peer []peer.Peer; err error}")
		}
		return typed.peers, typed.err
	}
	peers := []peer.Peer{}
	for _, p := range ps.peers {
		peers = append(peers, p)
	}
	return peers, nil
}

func (ps *testPeerStore) Find(id peer.ID) (peer.Peer, error) {
	p, ok := ps.peers[id]
	if !ok {
		return nil, fmt.Errorf("No peer: %s", id)
	}
	return p, nil
}

func (ps *testPeerStore) Self() (peer.Peer, error) {
	ret, ok := ps.actions["Self"]
	if ok {
		typedRet, ok := ret.(struct {
			self peer.Peer
			err  error
		})
		if !ok {
			return nil, errors.New("Self action must be of type struct{self peer.Peer; err error}")
		}
		return typedRet.self, typedRet.err
	}
	return ps.self, nil
}

type testConnection struct {
	id       connection.ID
	sender   peer.ID
	witness  peer.ID
	receiver peer.ID
	status   connection.Status
}

func (c *testConnection) ID() connection.ID {
	return c.id
}

func (c *testConnection) Receiver() peer.ID {
	return c.receiver
}

func (c *testConnection) Sender() peer.ID {
	return c.sender
}

func (c *testConnection) SetStatus(status connection.Status) (connection.Connection, error) {
	updated := &testConnection{
		id:       c.id,
		sender:   c.sender,
		witness:  c.witness,
		receiver: c.receiver,
		status:   status,
	}
	return updated, nil
}

func (c *testConnection) SetWitness(peerID peer.ID) (connection.Connection, error) {
	updated := &testConnection{
		id:       c.id,
		sender:   c.sender,
		witness:  peerID,
		receiver: c.receiver,
		status:   c.status,
	}
	return updated, nil
}

func (c *testConnection) Status() connection.Status {
	return c.status
}

func (c *testConnection) Witness() peer.ID {
	return c.witness
}

func (c *testConnection) String() string {
	return fmt.Sprintf("{%s %s %s %s %s}", c.id, c.sender, c.witness, c.receiver, c.status)
}

type testConnectionStore struct {
	conns map[connection.ID]*testConnection
}

func (cs *testConnectionStore) Accept(id connection.ID, sender peer.Peer, receiver peer.Peer, status connection.Status) error {
	if _, ok := cs.conns[id]; ok {
		return fmt.Errorf("Connecton %s already exists", id)
	}
	cs.conns[id] = &testConnection{id, sender.ID(), nil, receiver.ID(), status}
	return nil
}

func (cs *testConnectionStore) All() ([]connection.ID, error) {
	conns := []connection.ID{}
	for _, c := range cs.conns {
		conns = append(conns, c.id)
	}
	return conns, nil
}

func (cs *testConnectionStore) Create(sender peer.Peer, receiver peer.Peer, status connection.Status) (connection.ID, error) {
	id, err := connection.NewRandomID()
	if err != nil {
		return connection.ID{}, err
	}
	cs.conns[id] = &testConnection{id, sender.ID(), nil, receiver.ID(), status}
	return id, nil
}

func (cs *testConnectionStore) Read(id connection.ID, readFunc func(conn connection.Connection) error) error {
	conn, ok := cs.conns[id]
	if !ok {
		return fmt.Errorf("No conn %s", id)
	}
	return readFunc(conn)
}

func (cs *testConnectionStore) Update(id connection.ID, writeFunc func(conn connection.Connection) (connection.Connection, error)) error {
	conn, ok := cs.conns[id]
	if !ok {
		return fmt.Errorf("No conn %s", id)
	}
	updated, err := writeFunc(conn)
	if updated != nil {
		conn, ok = updated.(*testConnection)
		if !ok {
			return errors.New("writeFunction returned something other than a *testConnection which means someone probably changed the tests.")
		}
		cs.conns[id] = conn
	}
	return err
}

type testMessageStore struct{}

func (*testMessageStore) Add(message data.Message) error {
	panic("unimplemented")
}

func (*testMessageStore) All() ([]data.Message, error) {
	panic("unimplemented")
}

func (*testMessageStore) Find(connectionID connection.ID, sequenceNumber int) (data.Message, error) {
	panic("unimplemented")
}

type testRep struct {
	peerID peer.ID
	weak   int
	strong int
	reward int
	value  float64
}

func (r *testRep) PeerID() peer.ID {
	return r.peerID
}

func (r *testRep) Reward() reputation.Reputation {
	updated := &testRep{
		peerID: r.peerID,
		weak:   r.weak,
		strong: r.strong,
		reward: r.reward + 1,
		value:  r.value,
	}
	return updated
}

func (r *testRep) StrongPenalize() reputation.Reputation {
	updated := &testRep{
		peerID: r.peerID,
		weak:   r.weak,
		strong: r.strong + 1,
		reward: r.reward,
		value:  r.value,
	}
	return updated
}

func (r *testRep) Value() float64 {
	return r.value
}

func (r *testRep) WeakPenalize() reputation.Reputation {
	updated := &testRep{
		peerID: r.peerID,
		weak:   r.weak + 1,
		strong: r.strong,
		reward: r.reward,
		value:  r.value,
	}
	return updated
}

type testReputationStore struct {
	reps map[peer.ID]*testRep
}

func (rs *testReputationStore) Read(peerID peer.ID, readFunc func(rep reputation.Reputation) error) error {
	if _, ok := rs.reps[peerID]; !ok {
		rs.reps[peerID] = &testRep{}
	}
	return readFunc(rs.reps[peerID])
}

func (rs *testReputationStore) Update(peerID peer.ID, writeFunc func(rep reputation.Reputation) (reputation.Reputation, error)) error {
	if _, ok := rs.reps[peerID]; !ok {
		rs.reps[peerID] = &testRep{}
	}
	updated, err := writeFunc(rs.reps[peerID])
	if updated != nil {
		r, ok := updated.(*testRep)
		if !ok {
			return errors.New("writeFunction returned something other than a *testRep which means someone probably changed the tests.")
		}
		rs.reps[peerID] = r
	}
	return err
}

type exchangeAction = func(peer.Peer, transport.Options, any, any) (bool, int, error)

type sendAction = func(peer.Peer, transport.Options, any) (bool, error)

type testTransport struct {
	exchangeActions            []exchangeAction
	sendActions                []sendAction
	connectHandler             transport.ExchangeHandler[connection.ConnectRequest, connection.ConnectResponse]
	witnessHandler             transport.ExchangeHandler[connection.WitnessRequest, connection.WitnessResponse]
	witnessNotificationHandler transport.SendHandler[connection.WitnessNotification]
	closeHandler               transport.SendHandler[connection.CloseRequest]
}

func (tx *testTransport) Exchange(peer peer.Peer, options transport.Options, data any, response any) (int, error) {
	for _, ea := range tx.exchangeActions {
		match, count, err := ea(peer, options, data, response)
		if match {
			return count, err
		}
	}
	return 0, fmt.Errorf("No action for given exchange input: %v, %v, %s, %T", peer, options, data, response)
}

func (tx *testTransport) RegisterHandlers(
	connectHandler transport.ExchangeHandler[connection.ConnectRequest, connection.ConnectResponse],
	witnessHandler transport.ExchangeHandler[connection.WitnessRequest, connection.WitnessResponse],
	witnessNotificationHandler transport.SendHandler[connection.WitnessNotification],
	closeHandler transport.SendHandler[connection.CloseRequest],
) error {
	tx.connectHandler = connectHandler
	tx.witnessHandler = witnessHandler
	tx.witnessNotificationHandler = witnessNotificationHandler
	tx.closeHandler = closeHandler
	return nil
}

func (tx *testTransport) Send(peer peer.Peer, options transport.Options, data any) error {
	for _, sa := range tx.sendActions {
		match, err := sa(peer, options, data)
		if match {
			return err
		}
	}
	return fmt.Errorf("No action for given send input: %v, %v, %s", peer, options, data)
}

type connectHandlerResp struct {
	peerID  peer.ID
	request connection.ConnectRequest
	resp    connection.ConnectResponse
	err     error
}

type testConnectHandler struct {
	resps []connectHandlerResp
}

func (h *testConnectHandler) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	for _, r := range h.resps {
		if r.peerID == peerID && reflect.DeepEqual(request, r.request) {
			return r.resp, r.err
		}
	}
	return connection.ConnectResponse{}, errors.New("No action found for given peerID and request")
}

func (h *testConnectHandler) Options() transport.Options {
	return transport.Options{}
}

type witnessHandlerResp struct {
	peerID  peer.ID
	request connection.WitnessRequest
	resp    connection.WitnessResponse
	err     error
}

type testWitnessHandler struct {
	resps []witnessHandlerResp
}

func (h *testWitnessHandler) Handle(peerID peer.ID, request connection.WitnessRequest) (connection.WitnessResponse, error) {
	for _, r := range h.resps {
		if r.peerID == peerID && reflect.DeepEqual(request, r.request) {
			return r.resp, r.err
		}
	}
	return connection.WitnessResponse{}, errors.New("No action found for given peerID and request")
}

func (h *testWitnessHandler) Options() transport.Options {
	return transport.Options{}
}

type witnessNotificationHandlerResp struct {
	peerID  peer.ID
	request connection.WitnessNotification
	err     error
}

type testWitnessNotificationHandler struct {
	resps []witnessNotificationHandlerResp
}

func (h *testWitnessNotificationHandler) Handle(peerID peer.ID, request connection.WitnessNotification) error {
	for _, r := range h.resps {
		if r.peerID == peerID && reflect.DeepEqual(request, r.request) {
			return r.err
		}
	}
	return errors.New("No action found for given peerID and request")
}

func (h *testWitnessNotificationHandler) Options() transport.Options {
	return transport.Options{}
}

type closeHandlerResp struct {
	peerID  peer.ID
	request connection.CloseRequest
	err     error
}

type testCloseHandler struct {
	resps []closeHandlerResp
}

func (h *testCloseHandler) Handle(peerID peer.ID, request connection.CloseRequest) error {
	for _, r := range h.resps {
		if r.peerID == peerID && reflect.DeepEqual(request, r.request) {
			return r.err
		}
	}
	return errors.New("No action found for given peerID and request")
}

func (h *testCloseHandler) Options() transport.Options {
	return transport.Options{}
}

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
	l.addEntry(log.Debug(), fmtMsg, values...)
}
func (l *outputCapturingLog) Error(fmtMsg string, values ...any) {
	l.addEntry(log.Error(), fmtMsg, values...)
}
func (l *outputCapturingLog) Info(fmtMsg string, values ...any) {
	l.addEntry(log.Info(), fmtMsg, values...)
}
func (l *outputCapturingLog) Trace(fmtMsg string, values ...any) {
	l.addEntry(log.Trace(), fmtMsg, values...)
}
func (l *outputCapturingLog) Warn(fmtMsg string, values ...any) {
	l.addEntry(log.Warn(), fmtMsg, values...)
}

func formatLogEntries(l *outputCapturingLog) []string {
	strs := []string{}
	for _, e := range l.entries {
		strs = append(strs, fmt.Sprintf("\"%s\t%s\t%s\"", e.level, e.time, e.message))
	}
	return strs
}

func formatConns(conns map[connection.ID]*testConnection) []string {
	strs := []string{}
	for _, v := range conns {
		strs = append(strs, v.String())
	}
	return strs
}
