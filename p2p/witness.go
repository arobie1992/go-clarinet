package p2p

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/repository"
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
		dec, err := base64.RawStdEncoding.DecodeString(parts[2])
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
	sender := getSender(s)
	log.Log().Infof("Received witness stream from %s", sender)

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Failed to read request: %s", err.Error())
		EnsureReset(s)
		return
	}
	log.Log().Infof("Read raw witness request %s from %s", str, sender)

	var req WitnessRequest
	if err := DeserializeWitnessRequest(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize witness request %s from %s", str, sender)
		resp := WitnessResponse{ConnectResponseStatusRejected, WitnessResponseRejectReasonHasErrors, err.Error()}
		if _, err := s.Write(SerializeWitnessResponse(resp)); err != nil {
			log.Log().Errorf("Failed to write witness response %v to %s", resp, sender)
		}
		log.Log().Infof("Wrote witness response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed witness stream from %s", sender)
		return
	}
	log.Log().Infof("Successfully deserialized witness request from %s into %v", sender, req)

	conn := CreateWitnessingConnection(req.ConnID, req.Sender, req.Receiver)
	log.Log().Infof("Created witnessing connection %v", conn)
	tx := repository.GetDB().Save(&conn)
	if tx.Error != nil {
		log.Log().Errorf("Failed to save witnessing connection %v to database", conn)
		resp := WitnessResponse{WitnessResponseStatusRejected, WitnessResponseRejectReasonHasErrors, tx.Error.Error()}
		if _, err := s.Write(SerializeWitnessResponse(resp)); err != nil {
			log.Log().Errorf("Failed to write witness response %v to %s", resp, sender)
		}
		log.Log().Infof("Wrote witness response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed witness stream from %s", sender)
		return
	}
	log.Log().Infof("Saved witnessing connecction %v to database", conn)

	resp := WitnessResponse{WitnessResponseStatusAccepted, WitnessResponseRejectReasonNone, ""}
	_, err = s.Write(SerializeWitnessResponse(resp))
	if err != nil {
		log.Log().Errorf("Failed to write response: %s", err)
		EnsureReset(s)
	} else {
		EnsureClose(s)
		log.Log().Infof("Closed witness stream from %s", sender)
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
		dec, err := base64.RawStdEncoding.DecodeString(parts[1])
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
	sender := getSender(s)
	log.Log().Infof("Received witness notification stream from %s", sender)

	buf := bufio.NewReader(s)
	str, err := buf.ReadString(';')
	if err != nil {
		log.Log().Errorf("Failed to read request: %s", err.Error())
		EnsureReset(s)
		return
	}
	log.Log().Infof("Received raw witness notification %s from %s", str, sender)

	var req WitnessNotification
	if err := DeserializeWitnessNotification(&req, str); err != nil {
		log.Log().Errorf("Failed to deserialize witness notification %s from %s", str, sender)
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, err.Error()}
		if _, err := s.Write(SerializeWitnessNotificationResponse(resp)); err != nil {
			log.Log().Errorf("Failed to send witness notification response %v to %s: %s", resp, sender, err)
		}
		log.Log().Infof("Sent witness notification response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed witness notification stream from %s", sender)
		return
	}
	log.Log().Infof("Successfully deserialized witness notification from %s to %v", sender, req)

	conn := Connection{ID: req.ConnID}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		log.Log().Errorf("Failed to retreive connection %s from database", req.ConnID)
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, tx.Error.Error()}
		if _, err := s.Write(SerializeWitnessNotificationResponse(resp)); err != nil {
			log.Log().Errorf("Failed to send witness notification response %v to %s: %s", resp, sender, err)
		}
		log.Log().Infof("Sent witness notification response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed witness notification stream from %s", sender)
		return
	}
	log.Log().Infof("Successfully retrieved connection %s from database", conn.ID)

	conn.Witness = req.Witness
	log.Log().Infof("Set witness on connecction %s to %s", conn.ID, req.Witness)
	conn.Status = ConnectionStatusOpen
	if tx := repository.GetDB().Save(&conn); tx.Error != nil {
		log.Log().Errorf("Failed to save connection %s to database", req.ConnID)
		resp := WitnessNotificationResponse{WitnessNotificationStatusFailure, tx.Error.Error()}
		if _, err := s.Write(SerializeWitnessNotificationResponse(resp)); err != nil {
			log.Log().Errorf("Failed to send witness notification response %v to %s: %s", resp, sender, err)
		}
		log.Log().Infof("Sent witness notification response %v to %s without error", resp, sender)
		EnsureClose(s)
		log.Log().Infof("Closed witness notification stream from %s", sender)
		return
	}
	log.Log().Infof("Successfully saved connection %s to database", conn.ID)

	resp := WitnessNotificationResponse{WitnessNotificationStatusSuccess, ""}
	_, err = s.Write(SerializeWitnessNotificationResponse(resp))
	if err != nil {
		log.Log().Errorf("Failed to write response: %s", err)
		EnsureReset(s)
	} else {
		EnsureClose(s)
		log.Log().Infof("Closed witness notification stream from node %s", sender)
	}
}
