package libp2p

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/google/uuid"
	libp2pPeer "github.com/libp2p/go-libp2p/core/peer"
)

func (t *libp2pTransport) serialize(input any) ([]byte, error) {
	switch v := input.(type) {
	case connection.ConnectRequest:
		return t.serializeConnectRequest(v)
	case connection.ConnectResponse:
		return t.serializeConnectResponse(v)
	case connection.WitnessRequest:
		return t.serializeWitnessRequest(v)
	case connection.WitnessResponse:
		return t.serializeWitnessResponse(v)
	case connection.WitnessNotification:
		return t.serializeWitnessNotification(v)
	case connection.CloseRequest:
		return t.serializeCloseRequest(v)
	default:
		return nil, fmt.Errorf("Cannot serialize unrecognized type: %T", input)
	}
}

func (t *libp2pTransport) deserialize(in []byte, out any) error {
	t.l.Trace("Preeparing to deserialize data %s into type %T", in, out)
	switch v := out.(type) {
	case *connection.ConnectRequest:
		return t.deserializeConnectRequest(in, v)
	case *connection.ConnectResponse:
		return t.deserializeConnectResponse(in, v)
	case *connection.WitnessRequest:
		return t.deserializeWitnessRequest(in, v)
	case *connection.WitnessResponse:
		return t.deserializeWitnessResponse(in, v)
	case *connection.WitnessNotification:
		return t.deserializeWitnessNotification(in, v)
	case *connection.CloseRequest:
		return t.deserializeCloseRequest(in, v)
	default:
		return fmt.Errorf("Cannot deserialize unrecognized type: %T", out)
	}
}

func (t *libp2pTransport) serializeConnectRequest(r connection.ConnectRequest) ([]byte, error) {
	sender := encodePeerID(r.Sender)
	recvr := encodePeerID(r.Receiver)
	s := fmt.Sprintf("%s.%s.%s.%s;", r.ConnID, sender, recvr, r.Options.WitnessSelector)
	return []byte(s), nil
}

func (t *libp2pTransport) deserializeConnectRequest(in []byte, r *connection.ConnectRequest) error {
	parts, err := getParts(in, 4)
	if err != nil {
		return err
	}
	t.l.Trace("Got the following parts: %v", parts)
	connID, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized connection ID %s", connID)
	sender, err := decodePeerID(parts[1])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized sender %s", sender)
	recvr, err := decodePeerID(parts[2])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized receiver %s", recvr)
	ws, err := connection.ParseWitnessSelector(parts[3])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized witness selector %s", ws)
	r.ConnID = connection.ID(connID)
	r.Sender = sender
	r.Receiver = recvr
	r.Options = connection.Options{WitnessSelector: ws}
	t.l.Trace("Deserialized data into ConnectRequest %v", r)
	return nil
}

func (t *libp2pTransport) serializeConnectResponse(r connection.ConnectResponse) ([]byte, error) {
	s := fmt.Sprintf("%s.%s.%t.%s;", r.ConnID, t.serializeSlice(r.Errors), r.Accepted, t.serializeSlice(r.RejectReasons))
	return []byte(s), nil
}

func (t *libp2pTransport) deserializeConnectResponse(in []byte, r *connection.ConnectResponse) error {
	parts, err := getParts(in, 4)
	if err != nil {
		return err
	}
	t.l.Trace("Got the following parts: %v", parts)

	id, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized id %s", id)

	errs, err := t.deserializeSlice(parts[1])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized errors %v", err)

	accepted, err := strconv.ParseBool(parts[2])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized accepted %t", accepted)

	reasons, err := t.deserializeSlice(parts[3])
	if err != nil {
		return err
	}
	t.l.Trace("Deserialized reasons %v", reasons)
	r.ConnID = connection.ID(id)
	r.Errors = errs
	r.Accepted = accepted
	r.RejectReasons = reasons
	t.l.Trace("Deserialized data into ConnectResponse %v", r)
	return nil
}

func (t *libp2pTransport) serializeWitnessRequest(r connection.WitnessRequest) ([]byte, error) {
	sender := encodePeerID(r.Sender)
	recvr := encodePeerID(r.Receiver)
	s := fmt.Sprintf("%s.%s.%s.%s;", r.ConnID, sender, recvr, r.Options.WitnessSelector)
	return []byte(s), nil
}

func (t *libp2pTransport) deserializeWitnessRequest(in []byte, r *connection.WitnessRequest) error {
	parts, err := getParts(in, 4)
	if err != nil {
		return err
	}
	connID, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	sender, err := decodePeerID(parts[1])
	if err != nil {
		return err
	}
	recvr, err := decodePeerID(parts[2])
	if err != nil {
		return err
	}
	ws, err := connection.ParseWitnessSelector(parts[3])
	if err != nil {
		return err
	}
	r.ConnID = connection.ID(connID)
	r.Sender = sender
	r.Receiver = recvr
	r.Options = connection.Options{WitnessSelector: ws}
	return nil
}

func (t *libp2pTransport) serializeWitnessResponse(r connection.WitnessResponse) ([]byte, error) {
	s := fmt.Sprintf("%s.%s.%t.%s;", r.ConnID, t.serializeSlice(r.Errors), r.Accepted, t.serializeSlice(r.RejectReasons))
	return []byte(s), nil
}

func (t *libp2pTransport) deserializeWitnessResponse(in []byte, r *connection.WitnessResponse) error {
	parts, err := getParts(in, 4)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	errs, err := t.deserializeSlice(parts[1])
	if err != nil {
		return err
	}
	accepted, err := strconv.ParseBool(parts[2])
	if err != nil {
		return err
	}
	reasons, err := t.deserializeSlice(parts[3])
	if err != nil {
		return err
	}
	r.ConnID = connection.ID(id)
	r.Errors = errs
	r.Accepted = accepted
	r.RejectReasons = reasons
	return nil
}

func (t *libp2pTransport) serializeWitnessNotification(n connection.WitnessNotification) ([]byte, error) {
	return []byte(fmt.Sprintf("%s.%s;", n.ConnectionID, encodePeerID(n.Witness))), nil
}

func (t *libp2pTransport) deserializeWitnessNotification(in []byte, n *connection.WitnessNotification) error {
	parts, err := getParts(in, 2)
	if err != nil {
		return err
	}
	connID, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	wit, err := decodePeerID(parts[1])
	if err != nil {
		return err
	}
	n.ConnectionID = connection.ID(connID)
	n.Witness = wit
	return nil
}

func (t *libp2pTransport) serializeCloseRequest(r connection.CloseRequest) ([]byte, error) {
	return []byte(fmt.Sprintf("%s;", r.ConnID)), nil
}

func (t *libp2pTransport) deserializeCloseRequest(in []byte, r *connection.CloseRequest) error {
	parts, err := getParts(in, 1)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return err
	}
	r.ConnID = connection.ID(id)
	return nil
}

func getParts(in []byte, numParts int) ([]string, error) {
	s := string(in)
	if s[len(s)-1] != ';' {
		return nil, errors.New("No terminating ';'")
	}
	s = s[:len(s)-1]
	parts := strings.Split(s, ".")
	if len(parts) != numParts {
		return nil, fmt.Errorf("Incorrect number of parts. Expected: %d, Got: %d", numParts, len(parts))
	}
	return parts, nil
}

func encodePeerID(peerId peer.ID) string {
	if peerId == nil {
		return ""
	}
	return b64enc(peerId.String())
}

func decodePeerID(s string) (peer.ID, error) {
	if s == "" {
		return nil, nil
	}
	dec, err := b64dec(s)
	if err != nil {
		return nil, err
	}
	return libp2pPeer.Decode(dec)
}

func b64enc(plain string) string {
	return base64.RawStdEncoding.EncodeToString([]byte(plain))
}

func b64dec(enc string) (string, error) {
	b, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", nil
	}
	return string(b), nil
}

func (t *libp2pTransport) serializeSlice(plain []string) string {
	t.l.Trace("Preparing to serialize slice of length %d: %v", len(plain), plain)
	enc := []string{}
	for i, s := range plain {
		t.l.Trace("Encoding element %d: %s", i, s)
		enc = append(enc, b64enc(s))
	}
	t.l.Trace("Encoded slice of length %d: %v", len(enc), enc)
	return strings.Join(enc, ",")
}

func (t *libp2pTransport) deserializeSlice(enc string) ([]string, error) {
	t.l.Trace("Preparing to deserialize slice string '%s'", enc)
	if !strings.Contains(enc, ",") {
		t.l.Debug("Encoded slice does not contain any element separators so is empty array")
		return []string{}, nil
	}
	parts := strings.Split(enc, ",")
	t.l.Trace("Slice contains %d elements: %v", len(parts), parts)
	dec := []string{}
	for _, p := range parts {
		s, err := b64dec(p)
		if err != nil {
			return nil, fmt.Errorf("Failed to decode %s: %s", p, err)
		}
		dec = append(dec, s)
	}
	t.l.Trace("Decoded slice of length %d: %v", len(dec), dec)
	return dec, nil
}
