package p2p

import (
	"bufio"
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

func SendData(connID uuid.UUID, data []byte) error {
	conn := Connection{ID: connID}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		return tx.Error
	}
	log.Log().Infof("Found connection %v", conn)
	if conn.Status != ConnectionStatusOpen {
		return errors.New(fmt.Sprintf("Connection %s is not open.", connID))
	}
	// only save the conn if it's open
	defer repository.GetDB().Save(&conn)
	log.Log().Infof("Connecction %s is open so sending data", conn.ID)

	seqNo := conn.NextSeqNo
	conn.NextSeqNo += 1

	d := DataMessage{connID, seqNo, string(data), "", ""}
	log.Log().Infof("Signing data message")
	sig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s", d.ConnID, d.SeqNo, d.Data))
	if err != nil {
		return err
	}
	d.SendSig = sig

	log.Log().Info("Saving data message %s:%d to database", connID, seqNo)
	repository.GetDB().Create(&d)

	log.Log().Infof("Opening stream to %s for conn %s to send message %d", conn.Witness, conn.ID, d.SeqNo)
	s, err := OpenStream(conn.Witness, dataProtocolID)
	if err != nil {
		return err
	}
	log.Log().Infof("Successfully opened stream to %s for conn %s to send message %d", conn.Witness, conn.ID, d.SeqNo)

	_, err = s.Write(serializeDataMessage(d))
	if err != nil {
		EnsureReset(s)
		return errors.New("Failed to send data: " + err.Error())
	}
	log.Log().Infof("Sent message without error")
	EnsureClose(s)
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
	sender := getSender(s)
	log.Log().Infof("Received data stream from %s", sender)

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Error reading request: %s", err)
		EnsureReset(s)
		return
	}
	log.Log().Infof("Received raw data message %s from %s", str, sender)

	d := DataMessage{}
	if err := deserializeDataMessage(&d, str); err != nil {
		log.Log().Errorf("Failed to deserialize data message %s: %s", str, err.Error())
		EnsureReset(s)
		return
	}
	log.Log().Infof("Successfully deserialized data message from %s into %v", sender, d)

	conn := Connection{ID: d.ConnID}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		log.Log().Errorf("Failed to find connection %s: %s", d.ConnID, tx.Error.Error())
		EnsureReset(s)
		return
	}
	log.Log().Infof("Successfully found connection %v", conn)

	if conn.Status != ConnectionStatusOpen {
		log.Log().Errorf("Received data on unopen connection %s", conn.ID)
		EnsureReset(s)
		return
	}
	defer repository.GetDB().Create(&d)
	log.Log().Infof("Connection %s is open, so accept data", conn.ID)

	if conn.Receiver == GetFullAddr() {
		log.Log().Infof("Message %s:%d is addressed to me", d.ConnID, d.SeqNo)
		receiverReview(conn, d)
		log.Log().Infof("Finished reviewing message %s:%d is addressed to me", d.ConnID, d.SeqNo)
		EnsureClose(s)
		log.Log().Infof("Closing data connection from %s", sender)
		return
	} else if conn.Witness == GetFullAddr() {
		log.Log().Infof("I am witnessing so sign and forward message %s:%d", d.ConnID, d.SeqNo)
		witSig, err := cryptography.Sign(fmt.Sprintf("%s.%d.%s.%s", d.ConnID, d.SeqNo, d.Data, d.SendSig))
		if err != nil {
			log.Log().Errorf("Failed to sign data message %s:%d as witness: %s", d.ConnID, d.SeqNo, err.Error())
			EnsureReset(s)
			return
		}
		d.WitSig = witSig
		log.Log().Infof("Successfully signed message %s:%d as witness", d.ConnID, d.SeqNo)

		witnessReview(conn, d)
		log.Log().Infof("Finished reviewing message %s:%d is addressed to me", d.ConnID, d.SeqNo)

		log.Log().Infof("Opening stream to forward message %s:%d to receiver %s", d.ConnID, d.SeqNo, conn.Receiver)
		fs, err := OpenStream(conn.Receiver, dataProtocolID)
		if err != nil {
			log.Log().Errorf("Failed to open stream to receiver: %s", err.Error())
			EnsureReset(s)
			return
		}
		log.Log().Infof("Successfully opened stream to forward message %s:%d to receiver %s", d.ConnID, d.SeqNo, conn.Receiver)

		_, err = fs.Write(serializeDataMessage(d))
		if err != nil {
			log.Log().Errorf("Failed to forward data to receiver: %s", err.Error())
			EnsureReset(s)
			EnsureReset(fs)
		} else {
			EnsureClose(s)
			EnsureClose(fs)
			log.Log().Infof("Closed data streams from %s and to %s", sender, conn.Receiver)
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
