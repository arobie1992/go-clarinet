package control

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"

	"github.com/arobie1992/go-clarinet/cryptography"
	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/p2p"
	"github.com/arobie1992/go-clarinet/repository"
	"github.com/arobie1992/go-clarinet/reputation"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/peer"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func RequestConnection(targetNode string) (uuid.UUID, error) {
	// create the logical connection
	log.Log().Infof("Making outgoing connection to node %s", targetNode)
	conn, err := p2p.CreateOutgoingConnection(targetNode)
	if err != nil {
		return uuid.UUID{}, err
	}
	log.Log().Infof("Created outgoing connection %s", conn.ID)

	log.Log().Infof("Sending connect request for connection %s to %s", conn.ID, targetNode)
	if err := sendConnectRequest(conn); err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return uuid.UUID{}, err
	}

	log.Log().Infof("Successfully connected to %s", conn.Receiver)
	conn.Status = p2p.ConnectionStatusRequestingWitness
	tx := repository.GetDB().Save(conn)
	if tx.Error != nil {
		return uuid.UUID{}, tx.Error
	}

	log.Log().Infof("Attempting to find witness for connection %s", conn.ID)
	witness, err := requestWitness(conn)
	if err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return uuid.UUID{}, err
	}

	log.Log().Infof("Successfully found witness for connection %s: %s", conn.ID, witness)
	conn.Witness = witness
	conn.Status = p2p.ConnectionStatusOpen
	if tx := repository.GetDB().Save(conn); tx.Error != nil {
		// TODO decide on how to handle failure to save
		return uuid.UUID{}, tx.Error
	}

	log.Log().Info("Notify receiver %s of witness %s for connection %s", targetNode, witness, conn.ID)
	if err := notifyReceiverOfWitness(conn); err != nil {
		conn.Status = p2p.ConnectionStatusClosed
		repository.GetDB().Save(conn)
		if err := sendCloseRequest(conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		if err := sendCloseRequest(conn, conn.Witness); err != nil {
			log.Log().Errorf("Close request failed: %s", err.Error())
		}
		return uuid.UUID{}, err
	}
	log.Log().Infof("Finished notifying receiver of witness for connection %s without error", conn.ID)
	return conn.ID, nil
}

func sendConnectRequest(conn *p2p.Connection) error {
	log.Log().Infof("Opening stream for connection %s to %s", conn.ID, conn.Receiver)
	s, err := p2p.OpenStream(conn.Receiver, p2p.ConnectProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()
	log.Log().Infof("Successfully opened stream for conn %s to %s", conn.ID, conn.Receiver)

	req := p2p.ConnectRequest{CT: p2p.ConnectionTypeSubscribe, WS: p2p.WitnessSelectorRequestor, ConnID: conn.ID}
	log.Log().Infof("Sending ConnectRequest for conn %s", conn.ID)
	_, err = s.Write([]byte(p2p.SerializeConnectRequest(req)))
	if err != nil {
		return err
	}
	log.Log().Infof("Wrote message without error")
	log.Log().Infof("Preparing to read response")
	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	log.Log().Infof("Received raw message %s", out)

	var resp p2p.ConnectResponse
	if err := p2p.DeserializeConnectResponse(&resp, string(out)); err != nil {
		return err
	}
	log.Log().Info("Deserialized ConnectResponse %v", resp)

	if resp.Status != p2p.ConnectResponseStatusAccepted {
		log.Log().Info("Connect request was not accepted")
		return errors.New(fmt.Sprintf("ConnectRequest not accepted: %s: %s", resp.Reason.Name(), resp.ErrMsg))
	}
	log.Log().Info("Connect request was accepted")

	return nil
}

func requestWitness(conn *p2p.Connection) (string, error) {
	log.Log().Infof("Finding wintess candidates for connection %s", conn.ID)
	candidates, err := getWitnessCandidates(conn)
	if err != nil {
		return "", err
	}
	log.Log().Infof("Found %d witness candidates", len(candidates))

	for len(candidates) > 0 {
		candidate := candidates[0]
		candidates = candidates[1:]
		addrs := p2p.GetLibp2pNode().Peerstore().Addrs(candidate)
		if len(addrs) == 0 {
			continue
		}

		// just try one address
		addr := addrs[0].String() + "/p2p/" + candidate.String()
		log.Log().Infof("Opening stream to %s to request as witness for conn %s", addr, conn.ID)
		s, err := p2p.OpenStream(addr, p2p.WitnessProtocolID)
		if err != nil {
			log.Log().Errorf("Error while requesting witness of %s: %s", addr, err)
			continue
		}
		log.Log().Infof("Successfully opened stream to %s to request as witness for conn %s", addr, conn.ID)

		req := p2p.WitnessRequest{ConnID: conn.ID, Sender: conn.Sender, Receiver: conn.Receiver}
		log.Log().Info("Sending witness request")
		_, err = s.Write(p2p.SerializeWitnessRequest(req))
		if err != nil {
			log.Log().Errorf("Error while sending witness request to %s: %s", addr, err)
			s.Reset()
			continue
		}
		log.Log().Info("Sent witness request without error")

		log.Log().Info("Preparing to read response")
		out, err := io.ReadAll(s)
		if err != nil {
			log.Log().Errorf("Error while reading response from %s: %s", addr, err)
			s.Reset()
			continue
		}
		log.Log().Infof("Got raw response %s", out)

		var resp p2p.WitnessResponse
		if err := p2p.DeserializeWitnessResponse(&resp, out); err != nil {
			log.Log().Errorf("Error while deserializing response %s from %s: %s", string(out), addr, err)
			if err := sendCloseRequest(conn, addr); err != nil {
				log.Log().Errorf("Error sending close request to witness candidate %s: %s", addr, err)
			}
			continue
		}
		log.Log().Infof("Deserialized response %v", resp)

		if resp.Status == p2p.WitnessResponseStatusAccepted {
			log.Log().Infof("Found witness for connection %s", conn.ID)
			return addr, nil
		}
		log.Log().Infof("Witness %s didn't accept witness request. Will try another.", candidate)
	}
	log.Log().Infof("Failed to find witness for connection %s", conn.ID)
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
	log.Log().Infof("Shuffling witness candidates for connection %s", conn.ID)
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

	log.Log().Infof("Opening connection to %s for conn %s to notify of witness %s", conn.Receiver, conn.ID, conn.Witness)
	s, err := p2p.OpenStream(conn.Receiver, p2p.WitnessNotificationProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()
	log.Log().Infof("Successfully opened connection to %s for conn %s to notify of witness %s", conn.Receiver, conn.ID, conn.Witness)

	req := p2p.WitnessNotification{ConnID: conn.ID, Witness: conn.Witness}
	log.Log().Infof("Sending witness notification %v", req)
	_, err = s.Write(p2p.SerializeWitnessNotification(req))
	if err != nil {
		return err
	}
	log.Log().Info("Sent witness notification without error")
	log.Log().Info("Preparing to read response")
	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	log.Log().Infof("Read raw response %s", out)

	var resp p2p.WitnessNotificationResponse
	if err := p2p.DeserializeWitnessNotificationResponse(&resp, out); err != nil {
		return err
	}
	log.Log().Infof("Deserialized witness notification response %v", resp)

	if resp.Status != p2p.WitnessNotificationStatusSuccess {
		log.Log().Infof("Failed to notify receiver of witness: %s", resp.FailMsg)
		return errors.New(fmt.Sprintf("Failed to notify receiver of witness: %s", resp.FailMsg))
	}
	log.Log().Infof("Successfully notified receiver %s of witness %s for connection %s", conn.Receiver, conn.Witness, conn.ID)

	return nil
}

func sendCloseRequest(conn *p2p.Connection, targetNode string) error {
	log.Log().Infof("Opening stream to %s to tell it to close conn %s", targetNode, conn.ID)
	s, err := p2p.OpenStream(targetNode, p2p.CloseProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()
	log.Log().Infof("Successfully opened stream to %s to tell it to close conn %s", targetNode, conn.ID)

	req := p2p.CloseRequest{ConnID: conn.ID}
	log.Log().Infof("Sending close request %v", req)
	_, err = s.Write([]byte(p2p.SerializeCloseRequest(req)))
	if err != nil {
		return err
	}
	log.Log().Info("Sent close request without errors")
	log.Log().Info("Preparing to read response")
	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	log.Log().Infof("Received raw response %s", out)

	var resp p2p.CloseResponse
	if err := p2p.DeserializeCloseResponse(&resp, string(out)); err != nil {
		return err
	}
	log.Log().Infof("Deserialized response to %v", resp)

	return nil
}

func QueryForMessage(nodeAddr string, conn p2p.Connection, seqNo int) ([]byte, []byte, error) {
	log.Log().Infof("Will query node %s for message %s:%d", nodeAddr, conn.ID, seqNo)
	refMsg := p2p.DataMessage{ConnID: conn.ID, SeqNo: seqNo}
	if tx := repository.GetDB().Find(&refMsg); tx.Error != nil {
		return []byte{}, []byte{}, tx.Error
	}
	if refMsg.Data == "" {
		return []byte{}, []byte{}, errors.New("Unable to find message.")
	}
	log.Log().Infof("Found ref message %v", refMsg)

	log.Log().Infof("Opening stream to %s to query for message %s:%d", nodeAddr, conn.ID, seqNo)
	s, err := p2p.OpenStream(nodeAddr, p2p.QueryProtocolID)
	if err != nil {
		return []byte{}, []byte{}, err
	}
	defer s.Close()
	log.Log().Infof("Successfully opened stream to %s to query for message %s:%d", nodeAddr, conn.ID, seqNo)

	req := p2p.QueryRequest{ConnID: conn.ID, SeqNo: seqNo}
	log.Log().Infof("Sending query message %v", req)
	if _, err := s.Write(p2p.SerializeQueryRequest(req)); err != nil {
		return []byte{}, []byte{}, err
	}
	log.Log().Info("Sent query message without error")

	log.Log().Info("Preparing to read response")
	out, err := io.ReadAll(s)
	if err != nil {
		return []byte{}, []byte{}, err
	}
	log.Log().Infof("Read raw response %s", out)

	var resp p2p.QueryResponse
	if err := p2p.DeserializeQueryResponse(&resp, out); err != nil {
		return []byte{}, []byte{}, err
	}
	log.Log().Infof("Deserialized query response into %v", resp)

	key, err := p2p.GetPeerKey(nodeAddr)
	if err != nil {
		return []byte{}, []byte{}, err
	}
	log.Log().Info("Got key for node %s", nodeAddr)

	valid, err := key.Verify([]byte(resp.MsgHash), []byte(resp.Sig))
	if !valid || err != nil {
		log.Log().Infof("Query response signature was invalid for message %s:%d to node %s", conn.ID, seqNo, nodeAddr)
		// the signature is invalid so don't even bother checking the message content
		reputation.StrongPenalize(nodeAddr)
		return p2p.SerializeQueryRequest(req), p2p.SerializeQueryResponse(resp), nil
	}

	if resp.MsgHash == p2p.HashMessage(refMsg) {
		log.Log().Infof("Signature and message match. Reward node.")
		reputation.Reward(nodeAddr)
		forwardTarget := filter(conn.Participants(), p2p.GetFullAddr(), nodeAddr)[0]
		log.Log().Infof("Forwarding query response to %s", forwardTarget)
		if err := forwardQueryResponse(refMsg, resp, nodeAddr, forwardTarget); err != nil {
			log.Log().Errorf("Failed to forward data message %s:%d query response to %s", req.ConnID, req.SeqNo, forwardTarget)
			return []byte{}, []byte{}, err
		}
		log.Log().Info("Successfully forwarded query response to %s", forwardTarget)
		return p2p.SerializeQueryRequest(req), p2p.SerializeQueryResponse(resp), nil
	}

	log.Log().Info("Signature was valid but content did not match")

	if p2p.GetFullAddr() == conn.Witness || nodeAddr == conn.Witness {
		log.Log().Info("Have enough context to apply strong penalty")
		// witnesses can always apply a strong penalty because they directly communicated with both ends
		// sender and receiver both communicated directly with the witness so they can also strongly penalize witness
		reputation.StrongPenalize(nodeAddr)
		return p2p.SerializeQueryRequest(req), p2p.SerializeQueryResponse(resp), nil
	}

	log.Log().Info("Not enough context so apply weak penalities")
	// otherwise this node and the queryied node did not direcctly communicate so need to weakly penalize both
	otherNodes := filter(conn.Participants(), p2p.GetFullAddr())
	for _, n := range otherNodes {
		reputation.WeakPenalize(n)
	}

	return p2p.SerializeQueryRequest(req), p2p.SerializeQueryResponse(resp), nil
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
	log.Log().Infof("Opening stream to %s to forward query response %v", forwardNodeAddr, resp)
	s, err := p2p.OpenStream(forwardNodeAddr, p2p.ForwardProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()
	log.Log().Infof("Successfully opnened stream to %s to forward query response %v", forwardNodeAddr, resp)

	fwdSig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s.%s.%s", d.ConnID, d.SeqNo, resp.MsgHash, resp.Sig, queriedNodeAddr))
	if err != nil {
		return err
	}
	
	f := p2p.QueryForward{ConnID: d.ConnID, SeqNo: d.SeqNo, MsgHash: resp.MsgHash, Sig: resp.Sig, Queried: queriedNodeAddr, FwdSig: fwdSig}
	log.Log().Infof("Added fowarder signature")
	_, err = s.Write(p2p.SerializeQueryForward(f))
	return err
}

func SendRequestPeersRequest(targetNode string, numPeers int) error {
	log.Log().Infof("Opening stream to %s to request %d peers", targetNode, numPeers)
	s, err := p2p.OpenStream(targetNode, p2p.RequestPeersProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()
	log.Log().Infof("Successfully opened stream to %s to request %d peers", targetNode, numPeers)

	req := p2p.RequestPeersRequest{NumPeers: numPeers}
	log.Log().Infof("Sending peer request %v", req)
	_, err = s.Write(p2p.SerializeRequestPeersRequest(req))
	if err != nil {
		return err
	}
	log.Log().Info("Sent peer request without error")

	log.Log().Info("Preparing to read response")
	out, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	log.Log().Infof("Read raw response %s", out)

	resp := p2p.RequestPeersResponse{}
	if err := p2p.DeserializeRequestPeersResponse(&resp, out); err != nil {
		return err
	}
	log.Log().Infof("Deserialized response into %v", resp)

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

type CloseError struct {
	Err           error
	SenderError   error
	WitnessError  error
	ReceiverError error
}

func (ce *CloseError) Error() string {
	return fmt.Sprintf("Err: %s, SenderError; %s, WitnessError: %s, ReceiverError: %s", ce.Err, ce.SenderError, ce.WitnessError, ce.ReceiverError)
}

func CloseConnection(connID uuid.UUID) *CloseError {
	err := repository.GetDB().Transaction(func(db *gorm.DB) error {
		log.Log().Infof("Attempting to close connection %s", connID)
		conn := p2p.Connection{ID: connID}
		if tx := db.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&conn); tx.Error != nil {
			return &CloseError{
				Err: errors.New(fmt.Sprintf("Failed to find connection: %s", tx.Error)),
			}
		}
		defer repository.GetDB().Save(&conn)
		log.Log().Infof("Found connection %v", conn)

		if conn.Status == p2p.ConnectionStatusClosed {
			// already closed so just honor the previous close
			log.Log().Infof("Connection %s already closed, just skip", conn.ID)
			return nil
		}

		var sendErr error = nil
		var witErr error = nil
		var recErr error = nil

		conn.Status = p2p.ConnectionStatusClosed
		for i, p := range conn.Participants() {
			if p == p2p.GetFullAddr() || p == "" {
				// skip self
				continue
			}
			log.Log().Infof("Sending close request for conn %s to %s", conn.ID, p)
			if err := sendCloseRequest(&conn, p); err != nil {
				switch i {
				case 0:
					sendErr = err
				case 1:
					witErr = err
				case 2:
					recErr = err
				default:
					log.Log().Fatalf("Connection %s has too many participants.", conn.ID)
				}
			}
		}

		if sendErr != nil || witErr != nil || recErr != nil {
			return &CloseError{
				SenderError:   sendErr,
				WitnessError:  witErr,
				ReceiverError: recErr,
			}
		}
		return nil
	})
	if err == nil {
		return nil
	}
	log.Log().Infof("There was an error during close %s", err.Error())
	var closeErr *CloseError
	if errors.As(err, &closeErr) {
		return closeErr
	}
	closeErr.Err = err
	return closeErr
}
