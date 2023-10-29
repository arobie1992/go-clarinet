package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-clarinet/cryptography"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/repository"
	"github.com/go-clarinet/reputation"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
	"gorm.io/gorm/clause"
)

func SendData(connID uuid.UUID, data []byte) error {
	conn := Connection{ID: connID}
	if tx := repository.GetDB().Clauses(clause.Locking{Strength: "UPDATE"}).Find(&conn); tx.Error != nil {
		return tx.Error
	}
	if conn.Status != ConnectionStatusOpen {
		return errors.New(fmt.Sprintf("Connection %s is not open.", connID))
	}
	// only save the conn if it's open
	defer repository.GetDB().Save(&conn)

	seqNo := conn.NextSeqNo
	conn.NextSeqNo += 1

	d := DataMessage{connID, seqNo, string(data), "", ""}
	sig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s", d.ConnID, d.SeqNo, d.Data))
	if err != nil {
		return err
	}
	d.SendSig = sig

	repository.GetDB().Create(&d)

	s, err := OpenStream(conn.Witness, dataProtocolID)
	if err != nil {
		return err
	}

	_, err = s.Write(serializeDataMessage(d))
	if err != nil {
		s.Reset()
		return errors.New("Failed to send data: " + err.Error())
	}
	s.Close()
	return nil
}

type DataMessage struct {
	ConnID  uuid.UUID
	SeqNo   int
	Data    string
	SendSig string
	WitSig  string
}

func serializeDataMessage(d DataMessage) []byte {
	enc := ""
	if d.Data != "" {
		enc = base64.RawStdEncoding.EncodeToString([]byte(d.Data))
	}
	encSig := base64.RawStdEncoding.EncodeToString([]byte(d.SendSig))
	encWit := ""
	if d.WitSig != "" {
		encWit = base64.RawStdEncoding.EncodeToString([]byte(d.WitSig))
	}
	return []byte(fmt.Sprintf("%s.%d.%s.%s.%s;", d.ConnID, d.SeqNo, enc, encSig, encWit))
}

func deserializeDataMessage(d *DataMessage, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid DataMessage format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 5 {
		return errors.New("Invalid DataMessage format: Incorrect number of segments.")
	}

	connID, err := uuid.Parse(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse ConnID: %s", err.Error())
		return errors.New("Invalid DataMessage format: Failed to parse ConnID.")
	}

	seqNo, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to parse SeqNo: %s", err.Error())
		return errors.New("Invalid DataMessage format: Failed to parse SeqNo.")
	}

	data := ""
	if parts[2] != "" {
		dec, err := base64.RawStdEncoding.DecodeString(parts[2])
		if err != nil {
			log.Log().Errorf("Failed to decode Data: %s", err.Error())
			return errors.New("Invalid DataMessage format: Failed to decode Data.")
		}
		data = string(dec)
	}

	decSig, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		log.Log().Errorf("Failed to decode SendSig: %s", err.Error())
		return errors.New("Invalid DataMessage format: Failed to decode SendSig.")
	}

	witSig := ""
	if parts[4] != "" {
		dec, err := base64.RawStdEncoding.DecodeString(parts[4])
		if err != nil {
			log.Log().Errorf("Failed to decode WitSig: %s", err.Error())
			return errors.New("Invalid DataMessage format: Failed to decode WitSig")
		}
		witSig = string(dec)
	}

	d.ConnID = connID
	d.SeqNo = seqNo
	d.Data = data
	d.SendSig = string(decSig)
	d.WitSig = witSig
	return nil
}

func dataStreamHandler(s network.Stream) {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		s.Reset()
		return
	}

	d := DataMessage{}
	if err := deserializeDataMessage(&d, str); err != nil {
		log.Log().Errorf("Failed to deserialize data message %s: %s", str, err.Error())
		s.Reset()
		return
	}

	conn := Connection{ID: d.ConnID}
	if tx := repository.GetDB().Clauses(clause.Locking{Strength: "UPDATE"}).Find(&conn); tx.Error != nil {
		log.Log().Errorf("Failed to find connection %s: %s", d.ConnID, err.Error())
		s.Reset()
		return
	}
	defer repository.GetDB().Save(&conn)

	if conn.Status != ConnectionStatusOpen {
		log.Log().Errorf("Received data on unopen connection %s", conn.ID)
		s.Reset()
		return
	}
	defer repository.GetDB().Create(&d)

	if conn.Receiver == GetFullAddr() {
		receiverReview(conn, d)
		s.Close()
		return
	} else if conn.Witness == GetFullAddr() {
		witSig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s.%s", d.ConnID, d.SeqNo, d.Data, d.SendSig))
		if err != nil {
			log.Log().Errorf("Failed to sign data message %s:%d as witness: %s", d.ConnID, d.SeqNo, err.Error())
			s.Reset()
			return
		}
		d.WitSig = witSig

		witnessReview(conn, d)

		fs, err := OpenStream(conn.Receiver, dataProtocolID)
		if err != nil {
			log.Log().Errorf("Failed to open stream to receiver: %s", err.Error())
			s.Reset()
			return
		}

		_, err = fs.Write(serializeDataMessage(d))
		if err != nil {
			log.Log().Errorf("Failed to forward data to receiver: %s", err.Error())
			s.Reset()
			fs.Reset()
		} else {
			s.Close()
			fs.Close()
		}
	} else {
		log.Log().Warnf("Received DataMessage %d for connection %s on which host is neither witness or receiver.", d.SeqNo, conn.ID)
	}
}

func witnessReview(conn Connection, d DataMessage) {
	if validSenderSig(conn, d) {
		reputation.Reward(conn.Sender)
	} else {
		reputation.StrongPenalize(conn.Sender)
	}
}

func receiverReview(conn Connection, d DataMessage) {
	penalized := false
	if !validWitnessSig(conn, d) {
		reputation.StrongPenalize(conn.Witness)
		penalized = true
	}
	if !validSenderSig(conn, d) {
		reputation.WeakPenalize(conn.Witness)
		reputation.WeakPenalize(conn.Sender)
		penalized = true
	}
	if !penalized {
		reputation.Reward(conn.Witness)
		reputation.Reward(conn.Sender)
	}
}

func validSenderSig(conn Connection, d DataMessage) bool {
	senderKey, err := GetPeerKey(conn.Sender)
	if err != nil {
		log.Log().Warnf("No available public key for sender on conn %s: %s", conn.ID, err)
		return false
	}
	valid, err := senderKey.Verify([]byte(fmt.Sprintf("%s.%d.%s", d.ConnID, d.SeqNo, d.Data)), []byte(d.SendSig))
	return valid && err == nil
}

func validWitnessSig(conn Connection, d DataMessage) bool {
	witKey, err := GetPeerKey(conn.Witness)
	if err != nil {
		log.Log().Warnf("No available public key for witness on conn %s: %s", conn.ID, err)
		return false
	}
	valid, err := witKey.Verify([]byte(fmt.Sprintf("%s.%d.%s.%s", d.ConnID, d.SeqNo, d.Data, d.SendSig)), []byte(d.WitSig))
	return valid && err == nil
}
