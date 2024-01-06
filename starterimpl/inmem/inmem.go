package inmem

import (
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/crypto"
	"github.com/arobie1992/go-clarinet/v2/data"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
)

type connEntry struct {
	lock sync.RWMutex
	conn connection.Connection
}

type inMemoryConnection struct {
	id       connection.ID
	sender   peer.ID
	witness  peer.ID
	receiver peer.ID
	status   connection.Status
}

func (c *inMemoryConnection) ID() connection.ID {
	return c.id
}

func (c *inMemoryConnection) Sender() peer.ID {
	return c.sender
}

func (c *inMemoryConnection) Witness() peer.ID {
	return c.witness
}

func (c *inMemoryConnection) SetWitness(peerID peer.ID) (connection.Connection, error) {
	return &inMemoryConnection{
		id:       c.id,
		sender:   c.sender,
		witness:  peerID,
		receiver: c.receiver,
		status:   c.status,
	}, nil
}

func (c *inMemoryConnection) Receiver() peer.ID {
	return c.receiver
}

func (c *inMemoryConnection) Status() connection.Status {
	return c.status
}

func (c *inMemoryConnection) SetStatus(status connection.Status) (connection.Connection, error) {
	return &inMemoryConnection{
		id:       c.id,
		sender:   c.sender,
		witness:  c.witness,
		receiver: c.receiver,
		status:   status,
	}, nil
}

type inMemoryConnectionStore struct {
	globalLock sync.RWMutex
	conns      map[connection.ID]*connEntry
}

func NewConnectionStore() connection.ConnectionStore {
	return &inMemoryConnectionStore{}
}

func (cs *inMemoryConnectionStore) Create(sender, receiver peer.Peer, status connection.Status) (connection.ID, error) {
	id, err := connection.NewRandomID()
	if err != nil {
		return id, err
	}
	err = cs.Accept(id, sender, receiver, status)
	return id, err
}

func (cs *inMemoryConnectionStore) Accept(id connection.ID, sender peer.Peer, receiver peer.Peer, status connection.Status) error {
	conn := inMemoryConnection{
		id:       id,
		sender:   sender.ID(),
		witness:  nil,
		receiver: receiver.ID(),
		status:   status,
	}
	cs.globalLock.Lock()
	defer cs.globalLock.Unlock()
	for _, ok := cs.conns[id]; ok; _, ok = cs.conns[id] {
		return fmt.Errorf("Collision occurred while generating ID for new cconnection. ID: %s", id)
	}
	cs.conns[id] = &connEntry{sync.RWMutex{}, &conn}
	return nil
}

func (cs *inMemoryConnectionStore) getConnEntry(id connection.ID) (*connEntry, error) {
	cs.globalLock.RLock()
	defer cs.globalLock.RUnlock()
	ce, ok := cs.conns[id]
	if !ok {
		return nil, fmt.Errorf("No connection exists for ID %s", id)
	}
	return ce, nil
}

func (cs *inMemoryConnectionStore) Read(id connection.ID, readFunc func(conn connection.Connection) error) error {
	ce, err := cs.getConnEntry(id)
	if err != nil {
		return err
	}
	ce.lock.RLock()
	defer ce.lock.RUnlock()
	return readFunc(ce.conn)
}

func (cs *inMemoryConnectionStore) Update(id connection.ID, writeFunc func(conn connection.Connection) (connection.Connection, error)) error {
	ce, err := cs.getConnEntry(id)
	if err != nil {
		return err
	}
	ce.lock.Lock()
	defer ce.lock.Unlock()
	updated, err := writeFunc(ce.conn)
	if updated != nil {
		ce.conn = updated
	}
	return err
}

type inMemoryMessage struct {
	connectionID   connection.ID
	sequenceNumber int
	data           []byte
	senderSig      []byte
	witnessSig     []byte
}

func (m inMemoryMessage) ConnectionID() connection.ID {
	return m.connectionID
}

func (m inMemoryMessage) SequenceNumber() int {
	return m.sequenceNumber
}

func (m inMemoryMessage) Data() []byte {
	return m.data
}

func (m inMemoryMessage) senderSegs() []byte {
	b64Data := base64.RawStdEncoding.EncodeToString(m.data)
	return []byte(fmt.Sprintf("%s.%d.%s", m.connectionID, m.sequenceNumber, b64Data))
}

func (m inMemoryMessage) SenderSign(privateKey crypto.PrivateKey) (data.Message, error) {
	sig, err := privateKey.Sign(m.senderSegs())
	return inMemoryMessage{
		connectionID:   m.connectionID,
		sequenceNumber: m.sequenceNumber,
		data:           m.data,
		senderSig:      sig,
		witnessSig:     m.witnessSig,
	}, err
}

func (m inMemoryMessage) VerifySender(publicKey crypto.PublicKey) (bool, error) {
	return publicKey.Verify(m.senderSegs(), m.senderSig)
}

func (m inMemoryMessage) witnessSegs() []byte {
	b64Sig := base64.RawStdEncoding.EncodeToString(m.senderSig)
	b64Data := base64.RawStdEncoding.EncodeToString(m.data)
	return []byte(fmt.Sprintf("%s.%d.%s.%s", m.connectionID, m.sequenceNumber, b64Data, b64Sig))
}

func (m inMemoryMessage) WitnessSign(privateKey crypto.PrivateKey) (data.Message, error) {
	sig, err := privateKey.Sign(m.witnessSegs())
	return inMemoryMessage{
		connectionID:   m.connectionID,
		sequenceNumber: m.sequenceNumber,
		data:           m.data,
		senderSig:      m.senderSig,
		witnessSig:     sig,
	}, err
}

func (m inMemoryMessage) VerifyWitness(publicKey crypto.PublicKey) (bool, error) {
	return publicKey.Verify(m.witnessSegs(), m.witnessSig)
}

func NewMessage() data.Message {
	return inMemoryMessage{}
}

type inMemoryMessageStore struct {
	globalLock sync.RWMutex
	messages   map[string]data.Message
}

func (ms *inMemoryMessageStore) genKey(connectionID connection.ID, seqNo int) string {
	return fmt.Sprintf("%s:%d", connectionID, seqNo)
}

func NewMessageStore() data.MessageStore {
	return &inMemoryMessageStore{}
}

func (ms *inMemoryMessageStore) Add(message data.Message) error {
	key := ms.genKey(message.ConnectionID(), message.SequenceNumber())
	ms.globalLock.Lock()
	defer ms.globalLock.Unlock()
	if _, ok := ms.messages[key]; ok {
		return fmt.Errorf("Message %s already exists", key)
	}
	ms.messages[key] = message
	return nil
}

func (ms *inMemoryMessageStore) All() ([]data.Message, error) {
	messages := []data.Message{}
	for _, m := range ms.messages {
		messages = append(messages, m)
	}
	return messages, nil
}

func (ms *inMemoryMessageStore) Find(connectionID connection.ID, sequenceNumber int) (data.Message, error) {
	key := ms.genKey(connectionID, sequenceNumber)
	m, ok := ms.messages[key]
	if !ok {
		return nil, fmt.Errorf("No message %s", key)
	}
	return m, nil
}

type proportionalReputation struct {
	peerID peer.ID
	good   float64
	total  float64
}

func (r proportionalReputation) PeerID() peer.ID {
	return r.peerID
}

func (r proportionalReputation) Reward() reputation.Reputation {
	return proportionalReputation{
		peerID: r.peerID,
		good:   r.good + 1,
		total:  r.total + 1,
	}
}

func (r proportionalReputation) StrongPenalize() reputation.Reputation {
	return proportionalReputation{
		peerID: r.peerID,
		good:   r.good,
		total:  r.total + 3,
	}
}

func (r proportionalReputation) Value() float64 {
	if r.total == 0 {
		return 1
	} else {
		return r.good / r.total
	}
}

func (r proportionalReputation) WeakPenalize() reputation.Reputation {
	return proportionalReputation{
		peerID: r.peerID,
		good:   r.good,
		total:  r.total + 1,
	}
}

type repEntry struct {
	lock sync.RWMutex
	rep  reputation.Reputation
}

type inMemoryReputationStore struct {
	globalLock sync.RWMutex
	reps       map[peer.ID]*repEntry
	createFunc func(peerID peer.ID) reputation.Reputation
}

func NewReputationStore(createFunc func(peerID peer.ID) reputation.Reputation) reputation.ReputationStore {
	if createFunc == nil {
		return &inMemoryReputationStore{
			createFunc: func(peerID peer.ID) reputation.Reputation {
				return proportionalReputation{peerID, 0, 0}
			},
		}
	}
	return &inMemoryReputationStore{createFunc: createFunc}
}

func (rs *inMemoryReputationStore) ensureRepExists(peerID peer.ID) *repEntry {
	if e, ok := rs.reps[peerID]; ok {
		return e
	}

	rs.globalLock.Lock()
	defer rs.globalLock.Unlock()
	if e, ok := rs.reps[peerID]; ok {
		return e
	}

	rs.reps[peerID] = &repEntry{sync.RWMutex{}, rs.createFunc(peerID)}
	if e, ok := rs.reps[peerID]; ok {
		return e
	}
	panic(fmt.Sprintf("Creation of reputation for peer %s failed.", peerID))
}

func (rs *inMemoryReputationStore) ReadOperation(peerID peer.ID, readFunc func(rep reputation.Reputation) error) error {
	e := rs.ensureRepExists(peerID)
	e.lock.RLock()
	defer e.lock.RUnlock()
	return readFunc(e.rep)
}

func (rs *inMemoryReputationStore) WriteOperation(peerID peer.ID, writeFunc func(rep reputation.Reputation) (reputation.Reputation, error)) error {
	e := rs.ensureRepExists(peerID)
	e.lock.Lock()
	e.lock.Unlock()
	updated, err := writeFunc(e.rep)
	if updated != nil {
		e.rep = updated
	}
	return err
}
