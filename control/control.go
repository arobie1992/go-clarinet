package control

import (
	"context"
	"io"
	"log"

	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"
)

func requestConnection(targetNode string) error {
	maddr, err := multiaddr.NewMultiaddr(targetNode)
	if err != nil {
		return err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}

	p2p.GetLibp2pNode().Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
	s, err := p2p.GetLibp2pNode().NewStream(context.Background(), info.ID, p2p.ConnectProtocolID)
	if err != nil {
		return err
	}

	conn, err := repository.CreateOutgoingConnection(targetNode)
	if err != nil {
		return err
	}

	req := p2p.ConnectRequest{CT: p2p.ConnectionTypeSubscribe, WS: p2p.WitnessSelectorRequestor, ConnID: conn.ID}
	_, err = s.Write([]byte(p2p.SerializeConnectRequest(req)))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	var resp p2p.ConnectResponse
	if err := p2p.DeserializeConnectResponse(&resp, string(out)); err != nil {
		return err
	}

	log.Printf("reply: %s\n", resp.String())
	return nil
}
