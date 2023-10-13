package control

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
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
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Println(err)
		s.Reset()
		return
	}

	var req peerConnectRequest
	if err := deserializePeerConnectRequest(&req, str); err != nil {
		resp := peerConnectResponse{rejected, hasErrors, err.Error()}
		s.Write([]byte(serializePeerConnectResponse(resp)))
		s.Reset()
		return
	}

	log.Printf("received: %s\n", req.String())

	resp := peerConnectResponse{accepted, none, ""}
	_, err = s.Write([]byte(serializePeerConnectResponse(resp)))
	if err != nil {
		log.Println(err)
		s.Reset()
	} else {
		s.Close()
	}
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
	s, err := GetLibP2pNode().NewStream(context.Background(), info.ID, ConnectProtocolID)
	if err != nil {
		return err
	}

	req := peerConnectRequest{subscribe, requestor}
	_, err = s.Write([]byte(serializePeerConnectRequest(req)))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	var resp peerConnectResponse
	if err := deserializePeerConnectResponse(&resp, string(out)); err != nil {
		return err
	}

	log.Printf("reply: %s\n", resp.String())
	return nil
}

type connectionType int

const (
	connect = iota
	subscribe
)

type witnessSelector int

const (
	requestor = iota
	target
)

type peerConnectRequest struct {
	ct connectionType
	ws witnessSelector
}

func (req *peerConnectRequest) String() string {
	ct := "unknown"
	switch connectionType(req.ct) {
	case connect:
		ct = "connect"
	case subscribe:
		ct = "subscribe"
	}

	ws := "unknown"
	switch witnessSelector(req.ws) {
	case requestor:
		ws = "requestor"
	case target:
		ws = "target"
	}

	return fmt.Sprintf("peerConnectRequest{ct: %s, ws: %s}", ct, ws)
}

func serializePeerConnectRequest(req peerConnectRequest) string {
	return strconv.Itoa(int(req.ct)) + "." + strconv.Itoa(int(req.ws)) + ";"
}

func deserializePeerConnectRequest(req *peerConnectRequest, msg string) error {
	ctTermPos := strings.Index(msg, ".")
	ct, err := strconv.Atoi(msg[:ctTermPos])
	if err != nil {
		return errors.New("Invalid message format")
	}

	wsTermPos := strings.Index(msg, ";")
	if wsTermPos <= ctTermPos {
		return errors.New("Invalid message format")
	}

	ws, err := strconv.Atoi(msg[ctTermPos+1 : wsTermPos])
	if err != nil {
		return errors.New("Invalid message format")
	}

	req.ct = connectionType(ct)
	req.ws = witnessSelector(ws)
	return nil
}

type connectResponseStatus int

const (
	accepted = iota
	rejected
)

type connectResponseRejectReason int

const (
	none = iota
	hasErrors
	unacceptableWS
)

type peerConnectResponse struct {
	status connectResponseStatus
	reason connectResponseRejectReason
	errMsg string
}

func (resp *peerConnectResponse) String() string {
	status := "unknown"
	switch connectResponseStatus(resp.status) {
	case accepted:
		status = "accepted"
	case rejected:
		status = "rejected"
	}

	reason := "unknown"
	switch connectResponseRejectReason(resp.reason) {
	case none:
		reason = "none"
	case hasErrors:
		reason = "hasErrors"
	case unacceptableWS:
		reason = "unacceptableWS"
	}
	return fmt.Sprintf("peerConnectResponse{status: %s, reason: %s, errMsg: \"%s\"}", status, reason, resp.errMsg)
}

func serializePeerConnectResponse(resp peerConnectResponse) string {
	enc := ""
	if resp.errMsg != "" {
		enc = base64.RawStdEncoding.EncodeToString([]byte(resp.errMsg))
	}
	return fmt.Sprintf("%d.%d.%s;", resp.status, resp.reason, enc)
}

func deserializePeerConnectResponse(resp *peerConnectResponse, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid peerConnectResponse format")
	}
	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 3 {
		return errors.New("Invalid peerConnectResponse format")
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}

	reason, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}

	errMsg := parts[2]
	if errMsg != "" {
		dec, err := base64.RawStdEncoding.DecodeString(errMsg)
		if err != nil {
			return err
		}
		errMsg = string(dec)
	}

	resp.status = connectResponseStatus(status)
	resp.reason = connectResponseRejectReason(reason)
	resp.errMsg = errMsg
	return nil
}
