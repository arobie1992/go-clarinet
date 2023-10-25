package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/go-clarinet/log"
	"github.com/go-clarinet/repository"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
	"gorm.io/gorm/clause"
)

func SendData(connID uuid.UUID, numBytes int) error {
	conn := Connection{ID: connID}
	if tx := repository.GetDB().Clauses(clause.Locking{Strength: "UPDATE"}).Find(&conn); tx.Error != nil {
		return tx.Error
	}
	defer repository.GetDB().Save(&conn)
	if conn.Status != ConnectionStatusOpen {
		return errors.New(fmt.Sprintf("Connection %s is not open.", connID))
	}

	seqNo := conn.NextSeqNo
	conn.NextSeqNo += 1

	data := DataMessage{connID, seqNo, makeRandomData(numBytes)}
	repository.GetDB().Create(&data)

	s, err := OpenStream(conn.Witness, dataProtocolID)
	if err != nil {
		return err
	}

	_, err = s.Write(serializeDataMessage(data))
	if err != nil {
		s.Reset()
		return errors.New("Failed to send data: " + err.Error())
	}
	s.Close()
	return nil
}

func makeRandomData(numBytes int) string {
	data := make([]byte, numBytes)
	for i := 0; i < numBytes; i++ {
		data[i] = byte(rand.Intn(128))
	}
	return string(data)
}

type DataMessage struct {
	ConnID uuid.UUID
	SeqNo  int
	Data   string
}

func serializeDataMessage(d DataMessage) []byte {
	enc := ""
	if d.Data != "" {
		enc = base64.RawStdEncoding.EncodeToString([]byte(d.Data))
	}
	return []byte(fmt.Sprintf("%s.%d.%s;", d.ConnID, d.SeqNo, enc))
}

func deserializeDataMessage(d *DataMessage, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid DataMessage format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 3 {
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
			log.Log().Errorf("Failed to decode sender: %s", err.Error())
			return errors.New("Invalid DataMessage format: Failed to decode sender.")
		}
		data = string(dec)
	}

	d.ConnID = connID
	d.SeqNo = seqNo
	d.Data = data
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

	if tx := repository.GetDB().Create(&d); tx.Error != nil {
		log.Log().Errorf("Failed to save data message %v: %s", d, err.Error())
		s.Reset()
		return
	}

	if conn.Receiver == GetFullAddr() {
		s.Close()
		return
	}

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
}
