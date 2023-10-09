package p2p

import (
	"bufio"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/go-clarinet/config"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
)

var libp2pNode host.Host
var fullAddr string
var once sync.Once

func GetLibp2pNode() host.Host {
	return libp2pNode
}

func GetFullAddr() string {
	return fullAddr
}

func InitLibp2pNode(config *config.Config) error {
	var retErr error = nil
	once.Do(func() {
		priv, err := getKey(config.Libp2p.CertPath)
		if err != nil {
			retErr = err
			return
		}

		node, err := libp2p.New(
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", config.Libp2p.Port)),
			libp2p.Identity(*priv),
			libp2p.DisableRelay(),
		)
		if err != nil {
			retErr = err
			return
		}

		node.SetStreamHandler(ConnectProtocolID, connectStreamHandler)
		node.SetStreamHandler(CloseProtocolID, closeStreamHandler)
		libp2pNode = node
		fullAddr = getHostAddress(GetLibp2pNode())
	})
	return retErr
}

func getKey(file string) (*crypto.PrivKey, error) {
	r, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("Invalid key format")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	libp2pKey, _, err := crypto.KeyPairFromStdKey(key)

	return &libp2pKey, nil
}

func getHostAddress(ha host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", ha.ID()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := ha.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

const ConnectProtocolID = "/connect"

func connectStreamHandler(s network.Stream) {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Println(err)
		s.Reset()
		return
	}

	var req ConnectRequest
	if err := DeserializeConnectRequest(&req, str); err != nil {
		resp := ConnectResponse{ConnectResponseStatusRejected, ConnectResponseRejectReasonHasErrors, err.Error()}
		s.Write([]byte(SerializeConnectResponse(resp)))
		s.Reset()
		return
	}

	log.Printf("received: %s\n", req.String())

	// TODO Save a new connection

	resp := ConnectResponse{ConnectResponseStatusAccepted, ConnectResponseRejectReasonNone, ""}
	_, err = s.Write([]byte(SerializeConnectResponse(resp)))
	if err != nil {
		log.Println(err)
		s.Reset()
	} else {
		s.Close()
	}
}

type ConnectionType int

const (
	ConnectionTypeConnect = iota
	ConnectionTypeSubscribe
)

type WitnessSelector int

const (
	WitnessSelectorRequestor = iota
	WitnessSelectorTarget
)

type ConnectRequest struct {
	CT     ConnectionType
	WS     WitnessSelector
	ConnID uuid.UUID
}

func (req *ConnectRequest) String() string {
	ct := "unknown"
	switch ConnectionType(req.CT) {
	case ConnectionTypeConnect:
		ct = "connect"
	case ConnectionTypeSubscribe:
		ct = "subscribe"
	}

	ws := "unknown"
	switch WitnessSelector(req.WS) {
	case WitnessSelectorRequestor:
		ws = "requestor"
	case WitnessSelectorTarget:
		ws = "target"
	}

	return fmt.Sprintf("peerConnectRequest{CT: %s, WS: %s, ConnId: %s}", ct, ws, req.ConnID)
}

func SerializeConnectRequest(req ConnectRequest) string {
	return fmt.Sprintf("%d.%d.%s;", req.CT, req.WS, req.ConnID)
}

func DeserializeConnectRequest(req *ConnectRequest, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid ConnectRequest format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 3 {
		return errors.New("Invalid ConnectRequest format: Incorrect number of segments.")
	}

	ct, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Printf("Failed to parse CT: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse CT.")
	}
	if ct != ConnectionTypeConnect && ct != ConnectionTypeSubscribe {
		log.Printf("Unrecognized CT: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Unrecognized CT.")
	}

	ws, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Printf("Failed to parse WS: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse WS.")
	}
	if ws != WitnessSelectorRequestor && ws != WitnessSelectorTarget {
		log.Printf("Unrecognized WS: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Unrecognized WS.")
	}

	connId, err := uuid.Parse(parts[2])
	if err != nil {
		log.Printf("Failed to parse ConnID: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse ConnID.")
	}

	req.CT = ConnectionType(ct)
	req.WS = WitnessSelector(ws)
	req.ConnID = connId
	return nil
}

type ConnectResponseStatus int

const (
	ConnectResponseStatusAccepted = iota
	ConnectResponseStatusRejected
)

type ConnectResponseRejectReason int

func (r ConnectResponseRejectReason) Name() string {
	switch r {
	case ConnectResponseRejectReasonNone:
		return "None"
	case ConnectResponseRejectReasonHasErrors:
		return "HasErrors"
	case ConnectResponseRejectReasonUnacceptableWS:
		return "UnacceptableWS"
	default:
		return "Unknown"
	}
}

const (
	ConnectResponseRejectReasonNone = iota
	ConnectResponseRejectReasonHasErrors
	ConnectResponseRejectReasonUnacceptableWS
)

type ConnectResponse struct {
	Status ConnectResponseStatus
	Reason ConnectResponseRejectReason
	ErrMsg string
}

func (resp *ConnectResponse) String() string {
	status := "unknown"
	switch ConnectResponseStatus(resp.Status) {
	case ConnectResponseStatusAccepted:
		status = "accepted"
	case ConnectResponseStatusRejected:
		status = "rejected"
	}

	return fmt.Sprintf("peerConnectResponse{status: %s, reason: %s, errMsg: \"%s\"}", status, resp.Reason.Name(), resp.ErrMsg)
}

func SerializeConnectResponse(resp ConnectResponse) string {
	enc := ""
	if resp.ErrMsg != "" {
		enc = base64.RawStdEncoding.EncodeToString([]byte(resp.ErrMsg))
	}
	return fmt.Sprintf("%d.%d.%s;", resp.Status, resp.Reason, enc)
}

func DeserializeConnectResponse(resp *ConnectResponse, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid ConnectResponse format: No terminating ';'.")
	}
	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 3 {
		return errors.New("Invalid ConnectResponse format: Incorrect number of segments.")
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Printf("Failed to parse status: %s\n", err.Error())
		return errors.New("Invalid ConnectResponse format: Failed to parse status.")
	}

	reason, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Printf("Failed to parse reason: %s\n", err.Error())
		return errors.New("Invalid ConnectResponse format: Failed to parse reason.")
	}

	errMsg := parts[2]
	if errMsg != "" {
		dec, err := base64.RawStdEncoding.DecodeString(errMsg)
		if err != nil {
			log.Printf("Failed to parse errMsg: %s\n", err.Error())
			return errors.New("Invalid ConnectResponse format: Failed to parse errMsg.")
		}
		errMsg = string(dec)
	}

	resp.Status = ConnectResponseStatus(status)
	resp.Reason = ConnectResponseRejectReason(reason)
	resp.ErrMsg = errMsg
	return nil
}

type CloseRequest struct {
	ConnID uuid.UUID
}

func SerializeCloseRequest(req CloseRequest) string {
	return fmt.Sprintf("%s;", req.ConnID)
}

func DeserializeCloseRequest(req *CloseRequest, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid ConnectRequest format: No terminating ';'.")
	}

	connID, err := uuid.Parse(msg[:len(msg)-1])
	if err != nil {
		log.Printf("Failed to parse ConnID: %s\n", err.Error())
		return errors.New("Invalid CloseRequest format: Failed to parse ConnID.")
	}

	req.ConnID = connID
	return nil
}

type CloseResponseStatus int

const (
	CloseResponseStatusSuccess = iota
	CloseResponseStatusFailure
)

type CloseResponse struct {
	Status  CloseResponseStatus
	FailMsg string
}

func SerializeCloseResponse(resp CloseResponse) string {
	return fmt.Sprintf("%d.%s;", resp.Status, base64.RawStdEncoding.EncodeToString([]byte(resp.FailMsg)))
}

func DeserializeCloseResponse(resp *CloseResponse, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid ConnectRequest format: No terminating ';'.")
	}

	status, err := strconv.Atoi(msg[:len(msg)-1])
	if err != nil {
		log.Printf("Failed to parse Status: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse Status.")
	}

	if status != CloseResponseStatusSuccess && status != CloseResponseStatusFailure {
		log.Printf("Unrecognized Status: %s\n", err.Error())
		return errors.New("Invalid ConnectRequest format: Unrecognized Status.")
	}

	resp.Status = CloseResponseStatus(status)
	return nil
}

const CloseProtocolID = "/close"

func closeStreamHandler(s network.Stream) {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Println(err)
		s.Reset()
		return
	}

	var req CloseRequest
	if err := DeserializeCloseRequest(&req, str); err != nil {
		resp := CloseResponse{CloseResponseStatusFailure, err.Error()}
		s.Write([]byte(SerializeCloseResponse(resp)))
		s.Reset()
		return
	}

	log.Printf("received: %v\n", req)

	// TODO find the connection and update its status to closed

	resp := CloseResponse{CloseResponseStatusSuccess, ""}
	_, err = s.Write([]byte(SerializeCloseResponse(resp)))
	if err != nil {
		log.Println(err)
		s.Reset()
	} else {
		s.Close()
	}
}
