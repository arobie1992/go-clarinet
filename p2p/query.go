package p2p

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-clarinet/cryptography"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/repository"
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

func serializeQueryResponse(resp QueryResponse) []byte {
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
	querierAddr := s.Conn().RemoteMultiaddr().String() + "/p2p/" + s.Conn().RemotePeer().String()

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		s.Reset()
		return
	}

	req := QueryRequest{}
	if err := deserializeQueryRequest(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize QueryRequest %s: %s", str, err.Error())
		s.Reset()
		return
	}

	d := DataMessage{ConnID: req.ConnID, SeqNo: req.SeqNo}
	// if we got an error, it doesn't really matter for the moment since we'll get penalized regardless
	// realistically would probably want better handling, like notifying the
	repository.GetDB().Find(&d)

	conn := Connection{req.ConnID, "", "", "", 0, 0}
	// similarly here, error doesn't really help, so just reply
	repository.GetDB().Find(&conn)

	msgHash := HashMessage(querierAddr, conn, d)
	sig, _ := cryptography.Sign(msgHash)

	rep := QueryResponse{MsgHash: msgHash, Sig: sig}
	s.Write(serializeQueryResponse(rep))
	s.Close()
}

func HashMessage(queryierAddr string, conn Connection, d DataMessage) string {
	// since the sender doesn't have the witness signature, need to customize the hash depending on whom we're talking to 
	var str string
	if conn.Sender == queryierAddr || conn.Sender == GetFullAddr() {
		str = fmt.Sprintf("%s.%d.%s.%s", d.ConnID, d.SeqNo, d.Data, string(d.SendSig))
	} else {
		str = fmt.Sprintf("%s.%d.%s.%s.%s", d.ConnID, d.SeqNo, d.Data, string(d.SendSig), string(d.WitSig))
	}
	hb := sha256.Sum256([]byte(str))
	return string(hb[:])
}
