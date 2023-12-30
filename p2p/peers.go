package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/arobie1992/go-clarinet/log"
	"github.com/libp2p/go-libp2p/core/network"
)

type RequestPeersRequest struct {
	NumPeers int
}

func SerializeRequestPeersRequest(req RequestPeersRequest) []byte {
	return []byte(fmt.Sprintf("%d;", req.NumPeers))
}

func deserializeRequestPeersRequest(req *RequestPeersRequest, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid RequestPeersRequest format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 1 {
		return errors.New("Invalid RequestPeersRequest format: Incorrect number of segments.")
	}

	numPeers, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to convert NumPeers: %s", err)
		return errors.New("Invalid RequestPeersRequest format: Failed to convert NumPeers.")
	}

	req.NumPeers = numPeers
	return nil
}

type RequestPeersResponse struct {
	PeerAddrs []string
}

func serializeRequestPeersResponse(resp RequestPeersResponse) []byte {
	enc := []string{}
	for _, addr := range resp.PeerAddrs {
		enc = append(enc, base64.RawStdEncoding.EncodeToString([]byte(addr)))
	}
	joined := strings.Join(enc, ",")
	return []byte(fmt.Sprintf("%s;", joined))
}

func DeserializeRequestPeersResponse(resp *RequestPeersResponse, msg []byte) error {
	str := string(msg)

	termIdx := strings.Index(str, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid RequestPeersResponse format: No terminating ';'.")
	}

	parts := strings.Split(str[:len(str)-1], ".")
	if len(parts) != 1 {
		return errors.New("Invalid RequestPeersResponse format: Incorrect number of segments.")
	}

	decAddrs := []string{}
	addrs := strings.Split(parts[0], ",")
	for i, addr := range addrs {
		decAddr, err := base64.RawStdEncoding.DecodeString(addr)
		if err != nil {
			log.Log().Errorf("Failed to decode Addr %d: %s", i, err)
			return errors.New("Invalid QueryForward format: Failed to decode Addr.")
		}
		decAddrs = append(decAddrs, string(decAddr))
	}

	resp.PeerAddrs = decAddrs
	return nil
}

func requestPeersStreamHandler(s network.Stream) {
	requestorAddr := s.Conn().RemoteMultiaddr().String() + "/p2p/" + s.Conn().RemotePeer().String()
	log.Log().Infof("Received request peers stream from %s", requestorAddr)

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		s.Reset()
		return
	}
	log.Log().Infof("Read raw peer request %s from %s", str, requestorAddr)

	req := RequestPeersRequest{}
	if err := deserializeRequestPeersRequest(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize RequestPeersRequest %s: %s", str, err.Error())
		s.Reset()
		return
	}
	log.Log().Infof("Deserialized request peers request from %s into %v", requestorAddr, req)

	peers := GetLibp2pNode().Peerstore().Peers()
	peerAddrs := []string{}
	for i, p := range peers {
		// at the moment, all peers should only have one address, so no need to differentiate between numPeers and numAddrs
		if i == req.NumPeers {
			break;
		}
		addrs := GetLibp2pNode().Peerstore().Addrs(p)
		for _, addr := range addrs {
			peerAddr := addr.String()+"/p2p/"+p.String()
			if peerAddr == GetFullAddr() || peerAddr == requestorAddr {
				// skip self & whoever's asking
				continue
			}
			peerAddrs = append(peerAddrs, peerAddr)
		}
	}

	resp := RequestPeersResponse{PeerAddrs: peerAddrs}
	if _, err := s.Write(serializeRequestPeersResponse(resp)); err != nil {
		log.Log().Errorf("Failed to write peer request response %v to %s: %s", resp, requestorAddr, err)
	}
	log.Log().Errorf("Write peer request response %v to %s without error", resp, requestorAddr, err)
	s.Close()
	log.Log().Infof("Closed peer request stream from %s", requestorAddr)
}
