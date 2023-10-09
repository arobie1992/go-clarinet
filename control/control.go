package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

func requestConnection(targetNode string) error {
	// create the logical connection
	conn, err := repository.CreateOutgoingConnection(targetNode)
	if err != nil {
		return err
	}

	if err := sendConnectRequest(conn); err != nil {
		conn.Status = repository.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn); err != nil {
			log.Printf("Close request failed: %s\n", err.Error())
		}
		return err
	}

	log.Printf("Successfully connected to %s\n", conn.Receiver)
	conn.Status = repository.ConnectionStatusRequestingReceiver
	repository.GetDB().Save(conn)

	// TODO begin the witness request process

	return nil
}

func sendConnectRequest(conn *repository.Connection) error {
	s, err := openStream(conn.Receiver, p2p.ConnectProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	req := p2p.ConnectRequest{CT: p2p.ConnectionTypeSubscribe, WS: p2p.WitnessSelectorRequestor, ConnID: conn.ID}
	_, err = s.Write([]byte(p2p.SerializeConnectRequest(req)))
	if err != nil {
		return err
	}

	// read the response and update the connection as appropriate
	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	var resp p2p.ConnectResponse
	if err := p2p.DeserializeConnectResponse(&resp, string(out)); err != nil {
		return err
	}

	if resp.Status != p2p.ConnectResponseStatusAccepted {
		return errors.New(fmt.Sprintf("ConnectRequest not accepted: %s: %s", resp.Reason.Name(), resp.ErrMsg))
	}

	return nil
}

func sendCloseRequest(conn *repository.Connection) error {
	s, err := openStream(conn.Receiver, p2p.CloseProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	req := p2p.CloseRequest{ConnID: conn.ID}
	_, err = s.Write([]byte(p2p.SerializeCloseRequest(req)))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}

	var resp p2p.CloseResponse
	if err := p2p.DeserializeCloseResponse(&resp, string(out)); err != nil {
		return err
	}

	return nil
}

func openStream(targetNode string, protocol protocol.ID) (network.Stream, error) {
	maddr, err := multiaddr.NewMultiaddr(targetNode)
	if err != nil {
		return nil, err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}

	p2p.GetLibp2pNode().Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
	return p2p.GetLibp2pNode().NewStream(context.Background(), info.ID, protocol)
}
