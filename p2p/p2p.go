package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arobie1992/go-clarinet/config"
	"github.com/arobie1992/go-clarinet/cryptography"
	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/repository"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"
	"gorm.io/gorm"
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
		var node host.Host
		var err error
		hostIp := config.Libp2p.Host
		if hostIp == "" {
			host, err := os.Hostname()
			if err != nil {
				retErr = err
				return
			}
			addrs, err := net.LookupHost(host)
			if err != nil {
				retErr = err
				return
			}
			for _, addr := range addrs {
				if net.ParseIP(addr).IsLoopback() {
					continue
				}
				hostIp = addr
				break
			}
		}

		addr := fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", hostIp, config.Libp2p.Port)

		if config.Libp2p.CertPath != "" {
			priv, err := cryptography.PrivKey()
			if err != nil {
				retErr = err
				return
			}
			node, err = libp2p.New(
				libp2p.ListenAddrStrings(addr),
				libp2p.Identity(*priv),
				libp2p.DisableRelay(),
			)
			if err != nil {
				retErr = err
				return
			}
		} else {
			node, err = libp2p.New(
				libp2p.ListenAddrStrings(addr),
				libp2p.DisableRelay(),
			)
			if err != nil {
				retErr = err
				return
			}
			cryptography.InitPrivKeyFromNode(node, getHostAddress(node))
		}

		node.SetStreamHandler(ConnectProtocolID, connectStreamHandler)
		node.SetStreamHandler(CloseProtocolID, closeStreamHandler)
		node.SetStreamHandler(WitnessProtocolID, witnessStreamHandler)
		node.SetStreamHandler(WitnessNotificationProtocolID, witnessNotificationStreamHandler)
		node.SetStreamHandler(dataProtocolID, dataStreamHandler)
		node.SetStreamHandler(QueryProtocolID, queryHandler)
		node.SetStreamHandler(ForwardProtocolID, forwardHandler)
		node.SetStreamHandler(RequestPeersProtocolID, requestPeersStreamHandler)
		libp2pNode = node
		fullAddr = getHostAddress(GetLibp2pNode())
	})
	return retErr
}

func getHostAddress(ha host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", ha.ID()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := ha.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

func getSender(s network.Stream) string {
	return s.Conn().RemoteMultiaddr().String() + "/p2p/" + s.Conn().RemotePeer().String()
}

func connectStreamHandler(s network.Stream) {
	sender := getSender(s)
	log.Log().Infof("Received connect stream from %s", sender)

	s.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		EnsureReset(s)
		return
	}
	log.Log().Infof("Received raw msg %s from %s", str, sender)

	var req ConnectRequest
	if err := DeserializeConnectRequest(&req, str); err != nil {
		resp := ConnectResponse{ConnectResponseStatusRejected, ConnectResponseRejectReasonHasErrors, err.Error()}
		s.Write([]byte(SerializeConnectResponse(resp)))
		EnsureClose(s)
		return
	}
	log.Log().Infof("Deserialized connect request from %s to %v", sender, req)

	conn := CreateIncomingConnection(req.ConnID, sender)
	log.Log().Infof("Created incoming connection %v", conn)
	tx := repository.GetDB().Save(&conn)
	if tx.Error != nil {
		log.Log().Infof("Failed to save conn %v", conn)
		resp := ConnectResponse{ConnectResponseStatusRejected, ConnectResponseRejectReasonHasErrors, tx.Error.Error()}
		log.Log().Infof("Writing error response %v", resp)
		if _, err := s.Write([]byte(SerializeConnectResponse(resp))); err != nil {
			log.Log().Errorf("Error writing response: %s", err)
		}
		log.Log().Info("Wrote response without error")
		EnsureClose(s)
		log.Log().Infof("Closed stream from %s", sender)
		return
	}
	log.Log().Infof("Saved conn %v", conn)

	resp := ConnectResponse{ConnectResponseStatusAccepted, ConnectResponseRejectReasonNone, ""}
	log.Log().Infof("Sending resp %v", resp)
	_, err = s.Write([]byte(SerializeConnectResponse(resp)))
	if err != nil {
		log.Log().Errorf("Error writing response: %s", err)
		EnsureReset(s)
	} else {
		EnsureClose(s)
		log.Log().Infof("Closed connect request stream from %s", sender)
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
		log.Log().Errorf("Failed to parse CT: %s", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse CT.")
	}
	if ct != ConnectionTypeConnect && ct != ConnectionTypeSubscribe {
		log.Log().Errorf("Unrecognized CT: %s", err.Error())
		return errors.New("Invalid ConnectRequest format: Unrecognized CT.")
	}

	ws, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to parse WS: %s", err.Error())
		return errors.New("Invalid ConnectRequest format: Failed to parse WS.")
	}
	if ws != WitnessSelectorRequestor && ws != WitnessSelectorTarget {
		log.Log().Errorf("Unrecognized WS: %s", err.Error())
		return errors.New("Invalid ConnectRequest format: Unrecognized WS.")
	}

	connId, err := uuid.Parse(parts[2])
	if err != nil {
		log.Log().Errorf("Failed to parse ConnID: %s", err.Error())
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
		log.Log().Errorf("Failed to parse status: %s", err.Error())
		return errors.New("Invalid ConnectResponse format: Failed to parse status.")
	}

	reason, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to parse reason: %s", err.Error())
		return errors.New("Invalid ConnectResponse format: Failed to parse reason.")
	}

	errMsg := parts[2]
	if errMsg != "" {
		dec, err := base64.RawStdEncoding.DecodeString(errMsg)
		if err != nil {
			log.Log().Errorf("Failed to parse errMsg: %s", err.Error())
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
		log.Log().Errorf("Failed to parse ConnID: %s", err.Error())
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
		return errors.New("Invalid CloseResponse format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 2 {
		return errors.New("Invalid CloseResponse format: Incorrect number of segments.")
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse Status: %s", err.Error())
		return errors.New("Invalid CloseResponse format: Failed to parse Status.")
	}

	if status != CloseResponseStatusSuccess && status != CloseResponseStatusFailure {
		log.Log().Errorf("Unrecognized Status: %s", err.Error())
		return errors.New("Invalid CloseResponse format: Unrecognized Status.")
	}

	failMsg := ""
	if parts[1] != "" {
		dec, err := base64.RawStdEncoding.DecodeString(parts[1])
		if err != nil {
			log.Log().Errorf("Failed to decode FailMsg: %s", err.Error())
			return errors.New("Invalid CloseResponse format: Failed to decode FailMsg.")
		}
		failMsg = string(dec)
	}

	resp.Status = CloseResponseStatus(status)
	resp.FailMsg = failMsg
	return nil
}

func closeStreamHandler(s network.Stream) {
	sender := getSender(s)
	log.Log().Infof("Received close stream from %s", sender)

	s.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Failed to read request: %s", err)
		EnsureReset(s)
		return
	}
	log.Log().Infof("Read raw message %s from %s", str, sender)

	var req CloseRequest
	if err := DeserializeCloseRequest(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize close request %s from %s", str, sender)
		resp := CloseResponse{CloseResponseStatusFailure, err.Error()}
		if _, err := s.Write([]byte(SerializeCloseResponse(resp))); err != nil {
			log.Log().Errorf("Failed to send close response %v to %s", resp, sender)
		}
		log.Log().Infof("Sent close response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed close stream from %s", sender)
		return
	}
	log.Log().Infof("Deserialized close request from %s into %v", sender, req)

	repository.GetDB().Transaction(func(db *gorm.DB) error {
		conn := Connection{ID: req.ConnID}
		tx := db.Find(&conn)
		if tx.Error != nil {
			log.Log().Errorf("Failed to retrieve connection %s from database to close it", req.ConnID)
			resp := CloseResponse{CloseResponseStatusFailure, tx.Error.Error()}
			if _, err := s.Write([]byte(SerializeCloseResponse(resp))); err != nil {
				log.Log().Errorf("Failed to send close response %v to %s", resp, sender)
			}
			log.Log().Infof("Sent close response %v to %s without error", resp, sender)
			EnsureClose(s)
			log.Log().Infof("Closed close stream from %s", sender)
			return tx.Error
		}

		conn.Status = ConnectionStatusClosed
		tx = repository.GetDB().Save(&conn)
		if tx.Error != nil {
			log.Log().Errorf("Failed to save connection %s to database to close it", req.ConnID)
			resp := CloseResponse{CloseResponseStatusFailure, tx.Error.Error()}
			if _, err := s.Write([]byte(SerializeCloseResponse(resp))); err != nil {
				log.Log().Errorf("Failed to send close response %v to %s", resp, sender)
			}
			log.Log().Infof("Sent close response %v to %s without error", resp, sender)
			EnsureClose(s)
			log.Log().Infof("Closed close stream from %s", sender)
			return tx.Error
		}

		resp := CloseResponse{CloseResponseStatusSuccess, ""}
		_, err = s.Write([]byte(SerializeCloseResponse(resp)))
		if err != nil {
			log.Log().Errorf("Failed to write response: %s", err.Error())
			EnsureReset(s)
			return err
		}
		EnsureClose(s)
		log.Log().Infof("Closed close stream from %s", sender)
		return nil
	})
}

func AddPeer(peerAddress string) (*peer.AddrInfo, error) {
	maddr, err := multiaddr.NewMultiaddr(peerAddress)
	if err != nil {
		return nil, err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}

	GetLibp2pNode().Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
	return info, nil
}

func GetPeerKey(peerAddress string) (crypto.PubKey, error) {
	maddr, err := multiaddr.NewMultiaddr(peerAddress)
	if err != nil {
		return nil, err
	}

	nodeID, err := maddr.ValueForProtocol(multiaddr.P_P2P)
	if err != nil {
		return nil, err
	}

	peerID, err := peer.Decode(nodeID)
	if err != nil {
		return nil, err
	}

	key := GetLibp2pNode().Peerstore().PubKey(peerID)
	if key == nil {
		return nil, errors.New(fmt.Sprintf("No key for peer %s", peerID))
	}
	return key, nil
}
