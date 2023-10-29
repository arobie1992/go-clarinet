package control

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"

	"github.com/go-clarinet/cryptography"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
	"github.com/go-clarinet/reputation"
	"github.com/libp2p/go-libp2p/core/peer"
)

func requestConnection(targetNode string) error {
	// create the logical connection
	conn, err := p2p.CreateOutgoingConnection(targetNode)
	if err != nil {
		return err
	}

	if err := sendConnectRequest(conn); err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return err
	}

	log.Log().Infof("Successfully connected to %s", conn.Receiver)
	conn.Status = p2p.ConnectionStatusRequestingWitness
	tx := repository.GetDB().Save(conn)
	if tx.Error != nil {
		// TODO decide on how to handle failure to save
		return tx.Error
	}

	witness, err := requestWitness(conn)
	if err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return err
	}
	conn.Witness = witness
	conn.Status = p2p.ConnectionStatusOpen
	if tx := repository.GetDB().Save(conn); tx.Error != nil {
		// TODO decide on how to handle failure to save
		return tx.Error
	}

	if err := notifyReceiverOfWitness(conn); err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		if err := sendCloseRequest(conn, conn.Witness); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return err
	}

	return nil
}

func sendConnectRequest(conn *p2p.Connection) error {
	s, err := p2p.OpenStream(conn.Receiver, p2p.ConnectProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

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

	if resp.Status != p2p.ConnectResponseStatusAccepted {
		return errors.New(fmt.Sprintf("ConnectRequest not accepted: %s: %s", resp.Reason.Name(), resp.ErrMsg))
	}

	return nil
}

func requestWitness(conn *p2p.Connection) (string, error) {
	candidates, err := getWitnessCandidates(conn)
	if err != nil {
		return "", err
	}

	for len(candidates) > 0 {
		candidate := candidates[0]
		candidates = candidates[1:]
		addrs := p2p.GetLibp2pNode().Peerstore().Addrs(candidate)
		if len(addrs) == 0 {
			continue
		}

		// just try one address
		addr := addrs[0].String() + "/p2p/" + candidate.String()
		s, err := p2p.OpenStream(addr, p2p.WitnessProtocolID)
		if err != nil {
			log.Log().Errorf("Error while requesting witness of %s: %s", addr, err)
			continue
		}

		req := p2p.WitnessRequest{ConnID: conn.ID, Sender: conn.Sender, Receiver: conn.Receiver}
		_, err = s.Write(p2p.SerializeWitnessRequest(req))
		if err != nil {
			log.Log().Errorf("Error while sending witness request to %s: %s", addr, err)
			s.Reset()
			continue
		}

		out, err := io.ReadAll(s)
		if err != nil {
			log.Log().Errorf("Error while reading response from %s: %s", addr, err)
			s.Reset()
			continue
		}
		var resp p2p.WitnessResponse
		if err := p2p.DeserializeWitnessResponse(&resp, out); err != nil {
			log.Log().Errorf("Error while deserializing response %s from %s: %s", string(out), addr, err)
			if err := sendCloseRequest(conn, addr); err != nil {
				log.Log().Errorf("Error sending close request to witness candidate %s: %s", addr, err)
			}
			continue
		}

		if resp.Status == p2p.WitnessResponseStatusAccepted {
			return addr, nil
		}
	}
	return "", errors.New("Failed to find witness.")
}

func getWitnessCandidates(conn *p2p.Connection) ([]peer.ID, error) {
	peers := p2p.GetLibp2pNode().Peerstore().Peers()
	reputations, err := reputation.GetAll(peers)
	if err != nil {
		return nil, err
	}

	candidates := []peer.ID{}
	for _, p := range peers {
		if strings.Contains(conn.Receiver, p.String()) || strings.Contains(conn.Sender, p.String()) {
			continue
		}
		if reputations.IsTrusted(p) {
			candidates = append(candidates, p)
		}
	}
	// shuffle the candidates
	for i := range candidates {
		j := rand.Intn(i + 1)
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	return candidates, nil
}

func notifyReceiverOfWitness(conn *p2p.Connection) error {
	if conn.Witness == "" {
		return errors.New("No witness")
	}

	s, err := p2p.OpenStream(conn.Receiver, p2p.WitnessNotificationProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	req := p2p.WitnessNotification{ConnID: conn.ID, Witness: conn.Witness}
	_, err = s.Write(p2p.SerializeWitnessNotification(req))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}

	var resp p2p.WitnessNotificationResponse
	if err := p2p.DeserializeWitnessNotificationResponse(&resp, out); err != nil {
		return err
	}

	if resp.Status != p2p.WitnessNotificationStatusSuccess {
		return errors.New(fmt.Sprintf("Failed to notify receiver of witness: %s", resp.FailMsg))
	}

	return nil
}

func sendCloseRequest(conn *p2p.Connection, targetNode string) error {
	s, err := p2p.OpenStream(targetNode, p2p.CloseProtocolID)
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

func queryForMessage(nodeAddr string, conn p2p.Connection, seqNo int) error {
	refMsg := p2p.DataMessage{ConnID: conn.ID, SeqNo: seqNo}
	if tx := repository.GetDB().Find(&refMsg); tx.Error != nil {
		return tx.Error
	}
	if refMsg.Data == "" {
		return errors.New("Unable to find message.")
	}

	s, err := p2p.OpenStream(nodeAddr, p2p.QueryProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	req := p2p.QueryRequest{ConnID: conn.ID, SeqNo: seqNo}
	if _, err := s.Write(p2p.SerializeQueryRequest(req)); err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	var resp p2p.QueryResponse
	if err := p2p.DeserializeQueryResponse(&resp, out); err != nil {
		return err
	}

	key, err := p2p.GetPeerKey(nodeAddr)
	if err != nil {
		return err
	}
	valid, err := key.Verify([]byte(resp.MsgHash), []byte(resp.Sig))
	if !valid || err != nil {
		// the signature is invalid so don't even bother checking the message content
		reputation.StrongPenalize(nodeAddr)
		return nil
	}

	if resp.MsgHash == p2p.HashMessage(refMsg) {
		reputation.Reward(nodeAddr)
		forwardTarget := filter(conn.Participants(), p2p.GetFullAddr(), nodeAddr)[0]
		if err := forwardQueryResponse(refMsg, resp, nodeAddr, forwardTarget); err != nil {
			log.Log().Errorf("Failed to forward data message %s:%d query response to %s", req.ConnID, req.SeqNo, forwardTarget)
			return err
		}
		return nil
	}

	if p2p.GetFullAddr() == conn.Witness || nodeAddr == conn.Witness {
		// witnesses can always apply a strong penalty because they directly communicated with both ends
		// sender and receiver both communicated directly with the witness so they can also strongly penalize witness
		reputation.StrongPenalize(nodeAddr)
		return nil
	}

	// otherwise this node and the queryied node did not direcctly communicate so need to weakly penalize both
	otherNodes := filter(conn.Participants(), p2p.GetFullAddr())
	for _, n := range otherNodes {
		reputation.WeakPenalize(n)
	}

	return nil
}

func filter(vals []string, exclusions ...string) []string {
	filtered := []string{}
	for _, v := range vals {
		shouldInclude := true
		for _, e := range exclusions {
			if v == e {
				shouldInclude = false
				break
			}
		}
		if shouldInclude {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func forwardQueryResponse(d p2p.DataMessage, resp p2p.QueryResponse, queriedNodeAddr, forwardNodeAddr string) error {
	s, err := p2p.OpenStream(forwardNodeAddr, p2p.ForwardProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	fwdSig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s.%s.%s", d.ConnID, d.SeqNo, resp.MsgHash, resp.Sig, queriedNodeAddr))
	if err != nil {
		return err
	}
	f := p2p.QueryForward{ConnID: d.ConnID, SeqNo: d.SeqNo, MsgHash: resp.MsgHash, Sig: resp.Sig, Queried: queriedNodeAddr, FwdSig: fwdSig}
	_, err = s.Write(p2p.SerializeQueryForward(f))
	return err
}

func sendRequestPeersRequest(targetNode string, numPeers int) error {
	s, err := p2p.OpenStream(targetNode, p2p.RequestPeersProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	req := p2p.RequestPeersRequest{NumPeers: numPeers}
	_, err = s.Write(p2p.SerializeRequestPeersRequest(req))
	if err != nil {
		return err
	}

	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	resp := p2p.RequestPeersResponse{}
	if err := p2p.DeserializeRequestPeersResponse(&resp, out); err != nil {
		return err
	}

	for i, addr := range resp.PeerAddrs {
		if i == numPeers {
			log.Log().Warnf("Peer %s sent back more peers than requested. Skipping remaining. Requested %d, Got %d", numPeers, targetNode)
			break
		}
		log.Log().Infof("Adding peer: %s", addr)
		p2p.AddPeer(addr)
	}

	return nil
}
