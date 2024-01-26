package libp2p

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
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
	l    log.Logger
}

func NewTransport(host host.Host, logger log.Logger) transport.Transport {
	return &libp2pTransport{host, logger}
}

func streamCleanupWrapper(l log.Logger, handler func(s network.Stream) error) func(network.Stream) {
	return func(s network.Stream) {
		p := s.Conn().RemotePeer()
		err := handler(s)
		if err != nil {
			l.Error("Resetting stream %s from %s due to error: %s", s.ID(), p, err)
			if err := s.Reset(); err != nil {
				l.Error("Failed to reset stream %s from %s: %s", s.ID(), p, err)
			}
		} else {
			if err := s.Close(); err != nil {
				if strings.Contains(err.Error(), "close called for canceled stream") {
					l.Debug("Should be non-problematic close error: %s", err)
					return
				}
				l.Error("Failed to close stream %s from %s: %s", s.ID(), p, err)
			}
		}
	}
}

type handlerAdapter[I any] struct {
	h transport.SendHandler[I]
}

func (h *handlerAdapter[I]) Handle(peerID peer.ID, req I) (any, error) {
	return nil, h.h.Handle(peerID, req)
}

func (h *handlerAdapter[I]) Options() transport.Options {
	return h.h.Options()
}

func readWriteWrapper[I, O any](t *libp2pTransport, req I, handler transport.ExchangeHandler[I, O], hasResp bool) func(network.Stream) error {
	return func(s network.Stream) error {
		options := handler.Options()
		t.l.Debug("Timeout: %s", options.Timeout)
		if options.Timeout != nil {
			s.SetDeadline(time.Now().Add(*options.Timeout))
		}

		readTimeout := 10 * time.Second
		if options.ReadTimeout != nil {
			readTimeout = *options.ReadTimeout
		}
		t.l.Debug("ReadTimeout: %s", readTimeout)
		s.SetReadDeadline(time.Now().Add(readTimeout))

		buf := bufio.NewReader(s)
		raw, err := buf.ReadBytes(';')
		if err != nil {
			return err
		}
		t.l.Trace("Read raw data: %s", raw)

		if err := t.deserialize(raw, &req); err != nil {
			return err
		}
		t.l.Trace("Successfully deserialized raw data.")

		resp, err := handler.Handle(peer.ID(s.Conn().RemotePeer()), req)
		if err != nil {
			return err
		}
		if !hasResp {
			return nil
		}
		t.l.Debug("Response from handler: %v", resp)

		enc, err := t.serialize(resp)
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
	adaptedConnectHandler := readWriteWrapper(t, connection.ConnectRequest{}, connectHandler, true)
	t.host.SetStreamHandler("/connect", streamCleanupWrapper(t.l, adaptedConnectHandler))

	adaptedWitnessHandler := readWriteWrapper(t, connection.WitnessRequest{}, witnessHandler, true)
	t.host.SetStreamHandler("/witness", streamCleanupWrapper(t.l, adaptedWitnessHandler))

	adaptedWitnessNotificationHandler := readWriteWrapper(
		t,
		connection.WitnessNotification{},
		&handlerAdapter[connection.WitnessNotification]{witnessNotificationHandler},
		false,
	)
	t.host.SetStreamHandler("/witness-notification", streamCleanupWrapper(t.l, adaptedWitnessNotificationHandler))

	adaptedCloseHandler := readWriteWrapper(t, connection.CloseRequest{}, &handlerAdapter[connection.CloseRequest]{closeHandler}, false)
	t.host.SetStreamHandler("/close", streamCleanupWrapper(t.l, adaptedCloseHandler))
	return nil
}

// Send adds the peer and all addresses to the Libp2p peerstore and then attempts to deliver the provided data to the peer. This is unidirectional.
//
// This operation uses options.Timeout as the timeout for opening the underlying libp2p network.Stream.
func (t *libp2pTransport) Send(peer peer.Peer, options transport.Options, data any) error {
	s, err := t.sendInternal(peer, options, data)
	if err != nil {
		if s != nil {
			s.Reset()
		}
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
	t.l.Debug("Preparing to exchange data with peer %s", peer.ID())
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

	if err := t.deserialize(recv, response); err != nil {
		return len(recv), fmt.Errorf("Failed to decode response: %s", err)
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
			"Unsupported data type: %T. Supported types are: [%T, %T, %T, %T]",
			data,
			connection.ConnectRequest{},
			connection.WitnessRequest{},
			connection.CloseRequest{},
			connection.WitnessNotification{},
		)
	}
	t.l.Debug("Protocol ID: %s", protocolId)

	s, err := t.openStream(peer, protocolId, options)
	if err != nil {
		return s, err
	}

	writeTimeout := 10 * time.Second
	if options.WriteTimeout != nil {
		writeTimeout = *options.WriteTimeout
	}
	s.SetWriteDeadline(time.Now().Add(writeTimeout))

	enc, err := t.serialize(data)
	if err != nil {
		return s, fmt.Errorf("Failed to encode data: %s", err)
	}

	if _, err = s.Write(enc); err != nil {
		return s, err
	}
	return s, nil
}

func (t *libp2pTransport) openStream(peer peer.Peer, protocolId protocol.ID, options transport.Options) (network.Stream, error) {
	timeout := 10 * time.Second
	if options.Timeout != nil {
		timeout = *options.Timeout
	}

	id, err := libp2pPeer.Decode(peer.ID().String())
	if err != nil {
		return nil, err
	}
	ctx, cf := context.WithTimeout(context.Background(), timeout)
	s, err := t.host.NewStream(ctx, id, protocolId)
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
	ps.log.Trace("Will add address %s for peer %s", addr, id)
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
	p := lp2pPeer{id, []peer.Address{}}
	if id == ps.selfId {
		for _, addr := range ps.host.Addrs() {
			p.addresses = append(p.addresses, peer.Address(addr.String()))
		}
	} else {
		libp2pID, err := libp2pPeer.Decode(id.String())
		if err != nil {
			return nil, err
		}

		for _, addr := range ps.host.Peerstore().Addrs(libp2pID) {
			p.addresses = append(p.addresses, peer.Address(addr.String()))
		}
	}
	return &p, nil
}

func (ps *libp2pPeerStore) All() ([]peer.Peer, error) {
	ps.log.Trace("Entering libp2pPeerStore All method")
	peers := []peer.Peer{}
	for _, libp2pID := range ps.host.Peerstore().Peers() {
		ps.log.Trace("Found peer %s", libp2pID)
		peer, err := ps.Find(libp2pID)
		if err != nil {
			return peers, err
		}
		peers = append(peers, peer)
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
