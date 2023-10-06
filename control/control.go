package control

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"
)

var libp2pNode host.Host
var nodeSet = false
var nodeLock = sync.Mutex{}

func SetLibp2pNode(node host.Host) error {
	nodeLock.Lock()
	defer nodeLock.Unlock()
	if nodeSet {
		return errors.New("libp2pNode was already set")
	}
	libp2pNode = node
	nodeSet = true
	return nil
}

func GetLibP2pNode() host.Host {
	return libp2pNode
}

const ConnectProtocolID = "/connect"

func ConnectStreamHandler(s network.Stream) {
	log.Println("server preparing to read")
	buf := bufio.NewReader(s)
	str, err := buf.ReadString('\n')
	log.Println("server finished read")
	if err != nil {
		log.Println(err)
		s.Reset()
		return
	}
	log.Printf("read: %s", str)
	_, err = s.Write([]byte("Greetings as well!"))
	if err != nil {
		log.Println(err)
		s.Reset()
	} else {
		s.Close()
	}
	GetLibP2pNode()
}

func requestConnection(targetNode string) error {
	maddr, err := multiaddr.NewMultiaddr(targetNode)
	if err != nil {
		return err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}

	GetLibP2pNode().Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
	log.Println("sender opening stream")
	s, err := GetLibP2pNode().NewStream(context.Background(), info.ID, ConnectProtocolID)
	if err != nil {
		return err
	}

	log.Println("sender saying hello")
	_, err = s.Write([]byte("Hello, friend!\n"))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}

	log.Printf("read reply: %q\n", out)
	return nil
}
