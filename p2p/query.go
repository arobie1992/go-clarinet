package p2p

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/arobie1992/go-clarinet/cryptography"
	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/repository"
	"github.com/arobie1992/go-clarinet/reputation"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
)

type QueryRequest struct {
	ConnID uuid.UUID
	SeqNo  int
}

func SerializeQueryRequest(req QueryRequest) []byte {
	return []byte(fmt.Sprintf("%s.%d;", req.ConnID, req.SeqNo))
}

func deserializeQueryRequest(req *QueryRequest, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid QueryResponse format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 2 {
		return errors.New("Invalid QueryResponse format: Incorrect number of segments.")
	}

	connID, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}

	seqNo, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}

	req.ConnID = connID
	req.SeqNo = seqNo
	return nil
}

type QueryResponse struct {
	MsgHash string
	Sig     string
}

func SerializeQueryResponse(resp QueryResponse) []byte {
	encHash := base64.RawStdEncoding.EncodeToString([]byte(resp.MsgHash))
	encSig := base64.RawStdEncoding.EncodeToString([]byte(resp.Sig))
	return []byte(fmt.Sprintf("%s.%s;", encHash, encSig))
}

func DeserializeQueryResponse(resp *QueryResponse, msg []byte) error {
	str := string(msg)

	termIdx := strings.Index(str, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid QueryResponse format: No terminating ';'.")
	}

	parts := strings.Split(str[:len(str)-1], ".")
	if len(parts) != 2 {
		return errors.New("Invalid QueryResponse format: Incorrect number of segments.")
	}

	msgHash, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to decode MsgHash: %s", err.Error())
		return errors.New("Invalid QueryResponse format: Failed to decode Data.")
	}

	sig, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to decode Sig: %s", err.Error())
		return errors.New("Invalid QueryResponse format: Failed to decode Data.")
	}

	resp.MsgHash = string(msgHash)
	resp.Sig = string(sig)
	return nil
}

func queryHandler(s network.Stream) {
	sender := getSender(s)
	log.Log().Infof("Received query stream from %s", sender)

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		s.Reset()
		return
	}
	log.Log().Infof("Received raw query request %s from %s", str, sender)

	req := QueryRequest{}
	if err := deserializeQueryRequest(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize QueryRequest %s: %s", str, err.Error())
		s.Reset()
		return
	}
	log.Log().Infof("Successfully deserialized query request from %s into %v", sender, req)

	d := DataMessage{ConnID: req.ConnID, SeqNo: req.SeqNo}
	// if we got an error, it doesn't really matter for the moment since we'll get penalized regardless
	// realistically would probably want better handling, like notifying the sender
	if tx := repository.GetDB().Find(&d); tx.Error != nil {
		log.Log().Errorf("Failed to retreve data message for query request %v from database", req)
	}
	log.Log().Errorf("Successfully retreved data message for query request %v from database", req)

	msgHash := HashMessage(d)
	sig, err := cryptography.Sign(msgHash)
	if err != nil {
		log.Log().Errorf("Failed to sign hash of message for query request %v: %s", req, err)
	}
	log.Log().Errorf("Successfully signed hash of message for query request %v: %s", req, err)

	rep := QueryResponse{MsgHash: msgHash, Sig: sig}
	if _, err := s.Write(SerializeQueryResponse(rep)); err != nil {
		log.Log().Errorf("Failed to write query response %v to %s", rep, sender)
	}
	log.Log().Errorf("Wrote query response %v to %s without error", rep, sender)

	s.Close()
	log.Log().Infof("Closed query stream from %s", sender)
}

func HashMessage(d DataMessage) string {
	/* Need to include sender signature to ensure that a malicious witness can't alter the sender 
	signature during data transmission and then escape detection of this malicious action during
	querying. Can't include witness signature because sender has no record of the witness's 
	signature so can't calculate a hash with that properly. */
	// Are there any ways the lack of witness signature can be exploited? Doesn't seem like it, but need to review more.
	str := fmt.Sprintf("%s.%d.%s.%s", d.ConnID, d.SeqNo, d.Data, d.SendSig)
	hb := sha256.Sum256([]byte(str))
	return string(hb[:])
}

type QueryForward struct {
	ConnID  uuid.UUID
	SeqNo   int
	MsgHash string
	Sig     string
	Queried string
	FwdSig  string
}

func SerializeQueryForward(f QueryForward) []byte {
	encHash := base64.RawStdEncoding.EncodeToString([]byte(f.MsgHash))
	encSig := base64.RawStdEncoding.EncodeToString([]byte(f.Sig))
	encQueried := base64.RawStdEncoding.EncodeToString([]byte(f.Queried))
	encFwdSig := base64.RawStdEncoding.EncodeToString([]byte(f.FwdSig))
	return []byte(fmt.Sprintf("%s.%d.%s.%s.%s.%s;", f.ConnID, f.SeqNo, encHash, encSig, encQueried, encFwdSig))
}

func deserializeQueryForward(f *QueryForward, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid QueryForward format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 6 {
		return errors.New("Invalid QueryForward format: Incorrect number of segments.")
	}

	connID, err := uuid.Parse(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse ConnID: %s", err)
		return errors.New("Invalid QueryForward format: Failed to parse ConnID.")
	}

	seqNo, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to parse SeqNo: %s", err)
		return errors.New("Invalid QueryForward format: Failed to parse SeqNo.")
	}

	msgHash, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		log.Log().Errorf("Failed to decode MsgHash: %s", err)
		return errors.New("Invalid QueryForward format: Failed to decode MsgHash.")
	}

	sig, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		log.Log().Errorf("Failed to decode Sig: %s", err)
		return errors.New("Invalid QueryForward format: Failed to decode Sig.")
	}

	queried, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		log.Log().Errorf("Failed to decode Queried: %s", err)
		return errors.New("Invalid QueryForward format: Failed to decode Queried.")
	}

	fwdSig, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		log.Log().Errorf("Failed to decode FwdSig: %s", err)
		return errors.New("Invalid QueryForward format: Failed to decode FwdSig.")
	}

	f.ConnID = connID
	f.SeqNo = seqNo
	f.MsgHash = string(msgHash)
	f.Sig = string(sig)
	f.Queried = string(queried)
	f.FwdSig = string(fwdSig)
	return nil
}

func forwardHandler(s network.Stream) {
	forwarderAddr := s.Conn().RemoteMultiaddr().String() + "/p2p/" + s.Conn().RemotePeer().String()
	log.Log().Infof("Received forwarded query stream from %s")

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		s.Reset()
		return
	}
	log.Log().Infof("Received raw query forward %s from %s", str, forwarderAddr)

	f := QueryForward{}
	if err := deserializeQueryForward(&f, str); err != nil {
		log.Log().Errorf("Failed to deserialize QueryForward %s: %s", str, err)
		s.Reset()
		return
	}
	log.Log().Infof("Successfully deserialized query forward from %s into %v", forwarderAddr, f)

	key, err := GetPeerKey(forwarderAddr)
	if err != nil {
		log.Log().Errorf("Failed to retrieve key for forwarding peer %s: %s", forwarderAddr, err)
		s.Reset()
		return
	}
	log.Log().Infof("Successfully got key for forwarder %s", forwarderAddr)

	valid, err := key.Verify([]byte(fmt.Sprintf("%s.%d.%s.%s.%s", f.ConnID, f.SeqNo, f.MsgHash, f.Sig, f.Queried)), []byte(f.FwdSig))
	if !valid || err != nil {
		log.Log().Warnf("Invalid %s signature on forward", forwarderAddr)
		reputation.StrongPenalize(forwarderAddr)
		s.Close()
		return
	}
	log.Log().Infof("Forwarder %s signature for %v is valid", forwarderAddr, f)

	key, err = GetPeerKey(f.Queried)
	if err != nil {
		log.Log().Errorf("Failed to retrieve key for queried peer %s: %s", f.Queried, err)
		s.Reset()
		return
	}
	log.Log().Infof("Successfully got key for queried node %s", f.Queried)

	valid, err = key.Verify([]byte(f.MsgHash), []byte(f.Sig))
	if !valid || err != nil {
		// peers must only forward a query response if the signature is valid
		log.Log().Warnf("Node %s forwarded a query response with an invalid signature", forwarderAddr)
		reputation.StrongPenalize(forwarderAddr)
		s.Close()
		return
	}
	log.Log().Warnf("Node %s forwarded query response has valid signature", forwarderAddr)

	refMsg := DataMessage{ConnID: f.ConnID, SeqNo: f.SeqNo}
	if tx := repository.GetDB().Find(&refMsg); tx.Error != nil {
		log.Log().Warnf("Error while querying for data message: %s", err)
		s.Reset()
		return
	}
	if refMsg.Data == "" {
		log.Log().Warnf("Unable to find message: %s:%d", f.ConnID, f.SeqNo)
		s.Reset()
		return
	}
	log.Log().Infof("Found ref message %v", refMsg)

	conn := Connection{ID: f.ConnID, Sender: "", Witness: "", Receiver: "", Status: -1, NextSeqNo: -1}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		log.Log().Warnf("Error while querying for connection: %s", err)
		s.Reset()
		return
	}
	if conn.Status == -1 {
		log.Log().Warnf("Unable to find connection: %s", f.ConnID)
		s.Reset()
		return
	}
	log.Log().Infof("Found connection %v", conn)

	if f.MsgHash == HashMessage(refMsg) {
		log.Log().Infof("Forwarded message %v matched ref message hash", f)
		s.Close()
		return
	}
	log.Log().Infof("Forwarded message %v did not match ref message", f)

	if GetFullAddr() == conn.Witness || f.Queried == conn.Witness {
		// witnesses can always apply a strong penalty because they directly communicated with both ends
		// sender and receiver both communicated directly with the witness so they can also strongly penalize witness
		log.Log().Infof("Have enough context to apply strong penalty for forwarded message %v", f)
		reputation.StrongPenalize(f.Queried)
		s.Close()
		log.Log().Infof("Closing forward stream from %s", forwarderAddr)
		return
	}

	// otherwise this node and the queryied node did not direcctly communicate so need to weakly penalize both
	otherNodes := filter(conn.Participants(), GetFullAddr())
	for _, n := range otherNodes {
		log.Log().Infof("Applying weak penalty to %s for forward %v", n, f)
		reputation.WeakPenalize(n)
	}

	s.Close()
	log.Log().Infof("Closing forward stream from %s", forwarderAddr)
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
