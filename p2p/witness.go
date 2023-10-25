package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-clarinet/log"
	"github.com/go-clarinet/repository"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
)

type WitnessRequest struct {
	ConnID   uuid.UUID
	Sender   string
	Receiver string
}

func SerializeWitnessRequest(req WitnessRequest) []byte {
	s := base64.RawStdEncoding.EncodeToString([]byte(req.Sender))
	r := base64.RawStdEncoding.EncodeToString([]byte(req.Receiver))
	return []byte(fmt.Sprintf("%s.%s.%s;", req.ConnID, s, r))
}

func DeserializeWitnessRequest(req *WitnessRequest, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid WitnessRequest format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 3 {
		return errors.New("Invalid WitnessRequest format: Incorrect number of segments.")
	}

	connID, err := uuid.Parse(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse ConnID: %s", err.Error())
		return errors.New("Invalid WitnessRequest format: Failed to parse ConnID.")
	}

	s, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to decode sender: %s", err.Error())
		return errors.New("Invalid WitnessRequest format: Failed to decode sender.")
	}

	r, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		log.Log().Errorf("Failed to decode receiver: %s", err.Error())
		return errors.New("Invalid WitnessRequest format: failed to decode receiver.")
	}

	req.ConnID = connID
	req.Sender = string(s)
	req.Receiver = string(r)
	return nil
}

type WitnessResponseStatus int

const (
	WitnessResponseStatusAccepted = iota
	WitnessResponseStatusRejected
)

func (s WitnessResponseStatus) Name() string {
	switch s {
	case WitnessResponseStatusAccepted:
		return "Accepted"
	case WitnessResponseStatusRejected:
		return "Rejected"
	default:
		return "Unknown"
	}
}

type WitnessResponseRejectReason int

const (
	WitnessResponseRejectReasonNone = iota
	WitnessResponseRejectReasonHasErrors
)

type WitnessResponse struct {
	Status WitnessResponseStatus
	Reason WitnessResponseRejectReason
	ErrMsg string
}

func SerializeWitnessResponse(resp WitnessResponse) []byte {
	msg := ""
	if resp.ErrMsg != "" {
		msg = base64.RawStdEncoding.EncodeToString([]byte(resp.ErrMsg))
	}
	return []byte(fmt.Sprintf("%d.%d.%s;", resp.Status, resp.Reason, msg))
}

func DeserializeWitnessResponse(resp *WitnessResponse, msg []byte) error {
	str := string(msg)
	termIdx := strings.Index(str, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid WitnessResponse format: No terminating ';'.")
	}

	parts := strings.Split(str[:len(str)-1], ".")
	if len(parts) != 3 {
		return errors.New("Invalid WitnessResponse format: Incorrect number of segments.")
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse status: %s", err.Error())
		return errors.New("Invalid WitnessResponse format: Failed to parse status.")
	}

	reason, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to parse reason: %s", err.Error())
		return errors.New("Invalid WitnessResponse format: Failed to parse reason.")
	}

	errMsg := ""
	if parts[2] != "" {
		dec, err := base64.RawStdEncoding.DecodeString(parts[3])
		if err != nil {
			log.Log().Errorf("Failed to decode ErrMsg: %s", err.Error())
			return errors.New("Invalid WitnessResponse format: Failed to decode ErrMsg")
		}
		errMsg = string(dec)
	}

	resp.Status = WitnessResponseStatus(status)
	resp.Reason = WitnessResponseRejectReason(reason)
	resp.ErrMsg = errMsg
	return nil
}

func witnessStreamHandler(s network.Stream) {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Failed to read request: %s", err.Error())
		s.Reset()
		return
	}

	var req WitnessRequest
	if err := DeserializeWitnessRequest(&req, str); err != nil {
		resp := WitnessResponse{ConnectResponseStatusRejected, WitnessResponseRejectReasonHasErrors, err.Error()}
		s.Write(SerializeWitnessResponse(resp))
		s.Reset()
		return
	}

	conn := CreateWitnessingConnection(req.ConnID, req.Sender, req.Receiver)
	tx := repository.GetDB().Save(&conn)
	if tx.Error != nil {
		resp := WitnessResponse{WitnessResponseStatusRejected, WitnessResponseRejectReasonHasErrors, tx.Error.Error()}
		s.Write(SerializeWitnessResponse(resp))
		s.Reset()
		return
	}

	resp := WitnessResponse{WitnessResponseStatusAccepted, WitnessResponseRejectReasonNone, ""}
	_, err = s.Write(SerializeWitnessResponse(resp))
	if err != nil {
		log.Log().Errorf("Failed to write response: %s", err)
		s.Reset()
	} else {
		s.Close()
	}
}

type WitnessNotification struct {
	ConnID  uuid.UUID
	Witness string
}

func SerializeWitnessNotification(req WitnessNotification) []byte {
	w := base64.RawStdEncoding.EncodeToString([]byte(req.Witness))
	return []byte(fmt.Sprintf("%s.%s;", req.ConnID, w))
}

func DeserializeWitnessNotification(req *WitnessNotification, msg string) error {
	termIdx := strings.Index(msg, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid WitnessNotification format: No terminating ';'.")
	}

	parts := strings.Split(msg[:len(msg)-1], ".")
	if len(parts) != 2 {
		return errors.New("Invalid WitnessNotification format: Incorrect number of segments.")
	}

	connID, err := uuid.Parse(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse ConnID: %s", err.Error())
		return errors.New("Invalid WitnessNotification format: Failed to parse ConnID.")
	}

	dec, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		log.Log().Errorf("Failed to decode Witness: %s", err.Error())
		return errors.New("Invalid WitnessNotification format: Failed to decode Witness.")
	}

	req.ConnID = connID
	req.Witness = string(dec)
	return nil
}

type WitnessNotificationStatus int

const (
	WitnessNotificationStatusSuccess = iota
	WitnessNotificationStatusFailure
)

type WitnessNotificationResponse struct {
	Status  WitnessNotificationStatus
	FailMsg string
}

func SerializeWitnessNotificationResponse(resp WitnessNotificationResponse) []byte {
	enc := ""
	if resp.FailMsg != "" {
		enc = base64.RawStdEncoding.EncodeToString([]byte(resp.FailMsg))
	}
	return []byte(fmt.Sprintf("%d.%s;", resp.Status, enc))
}

func DeserializeWitnessNotificationResponse(resp *WitnessNotificationResponse, msg []byte) error {
	str := string(msg)
	termIdx := strings.Index(str, ";")
	if termIdx != len(msg)-1 {
		return errors.New("Invalid WitnessNotificationResponse format: No terminating ';'.")
	}

	parts := strings.Split(str[:len(str)-1], ".")
	if len(parts) != 2 {
		return errors.New("Invalid WitnessNotificationResponse format: Incorrect number of segments.")
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Log().Errorf("Failed to parse Status: %s", err.Error())
		return errors.New("Invalid WitnessNotificationResponse format: Failed to parse Status.")
	}

	failMsg := ""
	if parts[1] != "" {
		dec, err := base64.RawStdEncoding.DecodeString(parts[3])
		if err != nil {
			log.Log().Errorf("Failed to decode FailMsg: %s", err.Error())
			return errors.New("Invalid WitnessNotificationResponse format: Failed to decode FailMsg.")
		}
		failMsg = string(dec)
	}

	resp.Status = WitnessNotificationStatus(status)
	resp.FailMsg = failMsg
	return nil
}

func witnessNotificationStreamHandler(s network.Stream) {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Failed to read request: %s", err.Error())
		s.Reset()
		return
	}

	var req WitnessNotification
	if err := DeserializeWitnessNotification(&req, str); err != nil {
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, err.Error()}
		s.Write(SerializeWitnessNotificationResponse(resp))
		s.Reset()
		return
	}

	conn := Connection{ID: req.ConnID}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, tx.Error.Error()}
		s.Write(SerializeWitnessNotificationResponse(resp))
		s.Reset()
		return
	}

	conn.Witness = req.Witness
	conn.Status = ConnectionStatusOpen
	if tx := repository.GetDB().Save(&conn); tx.Error != nil {
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, tx.Error.Error()}
		s.Write(SerializeWitnessNotificationResponse(resp))
		s.Reset()
		return
	}

	resp := WitnessNotificationResponse{WitnessNotificationStatusSuccess, ""}
	_, err = s.Write(SerializeWitnessNotificationResponse(resp))
	if err != nil {
		log.Log().Errorf("Failed to write response: %s", err)
		s.Reset()
	} else {
		s.Close()
	}
}
