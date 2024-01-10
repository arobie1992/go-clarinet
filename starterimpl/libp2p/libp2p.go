package libp2p

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/transport"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	libp2pPeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

type libp2pTransport struct {
	host host.Host
}

func NewTransport(host host.Host) transport.Transport {
	return &libp2pTransport{host}
}

func streamCleanupWrapper(handler func(s network.Stream) error) func(network.Stream) {
	return func(s network.Stream) {
		err := handler(s)
		if err != nil {
			s.Reset()
		} else {
			s.Close()
		}
	}
}

type handlerAdapter[I any] struct {
	closeHandler transport.SendHandler[I]
}

func (h *handlerAdapter[I]) Handle(peerID peer.ID, req I) (any, error) {
	return nil, h.closeHandler.Handle(peerID, req)
}

func (h *handlerAdapter[I]) Options() transport.Options {
	return h.closeHandler.Options()
}

func readWriteWrapper[I, O any](req I, handler transport.ExchangeHandler[I, O]) func(network.Stream) error {
	return func(s network.Stream) error {
		options := handler.Options()
		if options.Timeout != nil {
			s.SetDeadline(time.Now().Add(*options.Timeout))
		}

		readTimeout := 10 * time.Second
		if options.ReadTimeout != nil {
			readTimeout = *options.ReadTimeout
		}
		s.SetReadDeadline(time.Now().Add(readTimeout))

		buf := bufio.NewReader(s)
		raw, err := buf.ReadBytes(';')
		if err != nil {
			return err
		}

		if err := base64JSONDeserialize(raw, &req); err != nil {
			return err
		}

		resp, err := handler.Handle(peer.ID(s.Conn().RemotePeer()), req)
		if err != nil {
			return err
		}

		enc, err := base64JSONSerialize(resp)
		if err != nil {
			return err
		}

		writeTimeout := 10 * time.Second
		if options.WriteTimeout != nil {
			writeTimeout = *options.WriteTimeout
		}
		s.SetWriteDeadline(time.Now().Add(writeTimeout))

		written, err := s.Write(enc)
		if err != nil {
			return err
		}
		if written != len(enc) {
			return fmt.Errorf("Failed to write all bytes. Had: %d, Wrote: %d", len(enc), written)
		}
		return nil
	}
}

func (t *libp2pTransport) RegisterHandlers(
	connectHandler transport.ConnectHandler,
	witnessHandler transport.WitnessHandler,
	witnessNotificationHandler transport.WitnessNotificationHandler,
	closeHandler transport.CloseHandler,
) error {
	// TODO is it worth seeing about handling the errors and allowing them to respond or should the errors only support the part of the logic that
	// the user provides?
	adaptedConnectHandler := readWriteWrapper(connection.ConnectRequest{}, connectHandler)
	t.host.SetStreamHandler("/connect", streamCleanupWrapper(adaptedConnectHandler))

	adaptedWitnessHandler := readWriteWrapper(connection.WitnessRequest{}, witnessHandler)
	t.host.SetStreamHandler("/witness", streamCleanupWrapper(adaptedWitnessHandler))

	adaptedWitnessNotificationHandler := readWriteWrapper(
		connection.WitnessNotification{},
		&handlerAdapter[connection.WitnessNotification]{witnessNotificationHandler},
	)
	t.host.SetStreamHandler("/witness-notification", streamCleanupWrapper(adaptedWitnessNotificationHandler))

	adaptedCloseHandler := readWriteWrapper(connection.CloseRequest{}, &handlerAdapter[connection.CloseRequest]{closeHandler})
	t.host.SetStreamHandler("/close", streamCleanupWrapper(adaptedCloseHandler))
	return nil
}

// Send adds the peer and all addresses to the Libp2p peerstore and then attempts to deliver the provided data to the peer. This is unidirectional.
//
// This operation uses options.Timeout as the timeout for opening the underlying libp2p network.Stream.
func (t *libp2pTransport) Send(peer peer.Peer, options transport.Options, data any) error {
	s, err := t.sendInternal(peer, options, data)
	if err != nil {
		s.Reset()
		return err
	}
	s.Close()
	return nil
}

// Send adds the peer and all addresses to the Libp2p peerstore, attempts to deliver the provided data to the peer,
// and finally attempts to receive a response from the peer.
//
// This operation uses options.Timeout as the timeout for opening the underlying libp2p network.Stream.
func (t *libp2pTransport) Exchange(peer peer.Peer, options transport.Options, data any, response any) (int, error) {
	s, err := t.sendInternal(peer, options, data)
	if err != nil {
		if s != nil {
			s.Reset()
		}
		return 0, err
	}

	readTimeout := 10 * time.Second
	if options.ReadTimeout != nil {
		readTimeout = *options.ReadTimeout
	}
	s.SetReadDeadline(time.Now().Add(readTimeout))

	recv, err := io.ReadAll(s)
	if err != nil {
		s.Reset()
		return len(recv), err
	}
	s.Close()

	if err := base64JSONDeserialize(recv, response); err != nil {
		return len(recv), fmt.Errorf("Failed to decode response. Most recent error: %s", err)
	}

	return len(recv), nil
}

func (t *libp2pTransport) sendInternal(peer peer.Peer, options transport.Options, data any) (network.Stream, error) {
	var protocolId protocol.ID
	switch data.(type) {
	case connection.ConnectRequest:
		protocolId = protocol.ID("/connect")
	case connection.WitnessRequest:
		protocolId = protocol.ID("/witness")
	case connection.CloseRequest:
		protocolId = protocol.ID("/close")
	case connection.WitnessNotification:
		protocolId = protocol.ID("/witness-notification")
	default:
		return nil, fmt.Errorf(
			"Unsupported data type: %T. Supported types are: [%T, %T, %T]",
			data,
			connection.ConnectRequest{},
			connection.WitnessRequest{},
			connection.CloseRequest{},
		)
	}

	s, err := t.openStream(peer, protocolId, options)
	if err != nil {
		return s, err
	}

	now := time.Now()
	if options.Timeout != nil {
		s.SetDeadline(now.Add(*options.Timeout))
	}

	writeTimeout := 10 * time.Second
	if options.WriteTimeout != nil {
		writeTimeout = *options.WriteTimeout
	}
	s.SetWriteDeadline(now.Add(writeTimeout))

	enc, err := base64JSONSerialize(data)
	if err != nil {
		return s, fmt.Errorf("Failed to encode data: %s", err)
	}

	if _, err = s.Write(enc); err != nil {
		return s, err
	}
	return s, nil
}

func base64JSONSerialize(value any) ([]byte, error) {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return []byte{}, err
	}
	data := base64.RawStdEncoding.EncodeToString(jsonData)
	enc := fmt.Sprintf("%s;", data)
	return []byte(enc), nil
}

func base64JSONDeserialize(data []byte, dest any) error {
	str := string(data)
	if str[len(str)-1] == ';' {
		str = str[:len(str)-1]
	}

	decoded, err := base64.RawStdEncoding.DecodeString(str)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(decoded, dest); err != nil {
		return err
	}
	return nil
}

func (t *libp2pTransport) openStream(peer peer.Peer, protocolId protocol.ID, options transport.Options) (network.Stream, error) {
	timeout := 10 * time.Second
	if options.Timeout != nil {
		timeout = *options.Timeout
	}

	ctx, cf := context.WithTimeout(context.Background(), timeout)
	s, err := t.host.NewStream(ctx, libp2pPeer.ID(peer.ID().String()), protocolId)
	cf()
	return s, err
}

type libp2pPeerStore struct {
	selfId peer.ID
	host   host.Host
	log    log.Logger
}

func NewPeerStore(host host.Host, log log.Logger) peer.PeerStore {
	return &libp2pPeerStore{peer.ID(host.ID()), host, log}
}

func (ps *libp2pPeerStore) AddAddr(id peer.ID, addr peer.Address) error {
	ps.log.Trace("Will add addreess %s for peer %s", addr, id)
	maddr, err := multiaddr.NewMultiaddr(string(addr))
	if err != nil {
		return err
	}
	ps.log.Trace("Parsed addr %s into multiaddr %s", addr, maddr)
	libp2pID, err := libp2pPeer.Decode(id.String())
	if err != nil {
		return err
	}
	ps.log.Trace("Parsed peer ID %s into libp2p peer ID %s", id, libp2pID)
	ps.host.Peerstore().AddAddr(libp2pID, maddr, peerstore.PermanentAddrTTL)
	return nil
}

func (ps *libp2pPeerStore) Find(id peer.ID) (peer.Peer, error) {
	addrs := []peer.Address{}
	for _, addr := range ps.host.Addrs() {
		addrs = append(addrs, peer.Address(addr.String()))
	}
	return &lp2pPeer{id, addrs}, nil
}

func (ps *libp2pPeerStore) All() ([]peer.Peer, error) {
	ps.log.Trace("Entering libp2pPeerStore All method")
	peers := []peer.Peer{}
	for _, libp2pID := range ps.host.Peerstore().Peers() {
		ps.log.Trace("Found peer %s", libp2pID)
		p := lp2pPeer{peer.ID(libp2pID), []peer.Address{}}
		ps.log.Trace("Created peer {%s %v}", p.id, p.addresses)
		peers = append(peers, &p)
		for _, addr := range ps.host.Peerstore().Addrs(libp2pID) {
			ps.log.Trace("Found address %s for peer %s", addr, p.id)
			p.addresses = append(p.addresses, peer.Address(addr.String()))
		}
	}
	ps.log.Trace("Compiled list of all peers: %v", peers)
	return peers, nil
}

func (ps *libp2pPeerStore) Self() (peer.Peer, error) {
	return ps.Find(ps.selfId)
}

type lp2pPeer struct {
	id        peer.ID
	addresses []peer.Address
}

func ParsePeerID(id string) (peer.ID, error) {
	return libp2pPeer.Decode(id)
}

func NewPeer(id peer.ID, addresses []peer.Address) peer.Peer {
	return &lp2pPeer{id, addresses}
}

func (p *lp2pPeer) ID() peer.ID {
	return p.id
}

func (p *lp2pPeer) Addresses() []peer.Address {
	return p.addresses
}
