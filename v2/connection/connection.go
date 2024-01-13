package connection

import (
	"fmt"

	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/google/uuid"
)

type ID uuid.UUID;

type Connection interface {
	ID() ID
	Sender() peer.ID
	Witness() peer.ID
	// SetWitness returns a connection that is identical to the receiver with the addition of having the witness value be the specified peerID.
	//
	// This operation return a Connection to allow immutable implementations, but implementations may also allow mutation. However, if an
	// implementation allows mutation, it must return the receiver. When using this method, it is best practice to discard the receiver
	// and only refer to the returned value from that point forward.
	SetWitness(peerID peer.ID) (Connection, error)
	Receiver() peer.ID
	Status() Status
	// SetStatus returns a connection that is identical to the receiver with the addition of having the status now equaling the supplied argument.
	//
	// This operation return a Connection to allow immutable implementations, but implementations may also allow mutation. However, if an
	// implementation allows mutation, it must return the receiver. When using this method, it is best practice to discard the receiver
	// and only refer to the returned value from that point forward.
	SetStatus(status Status) (Connection, error)
}

type Status interface {
	connectionStatus()
	String() string
}

type requestingReceiver struct{}
type requestingSender struct{}
type awaitingWitness struct{}
type requestingWitness struct{}
type notifyingOfWitness struct{}
type open struct{}
type closing struct{}
type closed struct{}

func (_ *requestingReceiver) connectionStatus() {}
func (_ *requestingSender) connectionStatus()   {}
func (_ *awaitingWitness) connectionStatus()    {}
func (_ *requestingWitness) connectionStatus()  {}
func (_ *notifyingOfWitness) connectionStatus() {}
func (_ *open) connectionStatus()               {}
func (_ *closing) connectionStatus()            {}
func (_ *closed) connectionStatus()             {}

func (_ *requestingReceiver) String() string { return "RequestingReceiver" }
func (_ *requestingSender) String() string   { return "RequestingSender" }
func (_ *requestingWitness) String() string  { return "RequestingWitness" }
func (_ *awaitingWitness) String() string    { return "AwaitingWitness" }
func (_ *notifyingOfWitness) String() string { return "NotifyingOfWitness" }
func (_ *open) String() string               { return "Open" }
func (_ *closing) String() string            { return "Closing" }
func (_ *closed) String() string             { return "Closed" }

var rr Status = &requestingReceiver{}
var rs Status = &requestingSender{}
var aw Status = &awaitingWitness{}
var rw Status = &requestingWitness{}
var now Status = &notifyingOfWitness{}
var o Status = &open{}
var cing Status = &closing{}
var c Status = &closed{}

func RequestingReceiver() Status { return rr }
func RequestingSender() Status   { return rs }
func AwaitingWitness() Status    { return aw }
func RequestingWitness() Status  { return rw }
func NotifyingOfWitness() Status { return now }
func Open() Status               { return o }
func Closing() Status            { return cing }
func Closed() Status             { return c }

// ConnetionStore is an interface for some backing storage for the Clarinet connections a node has.
// All operations on types implementing this interface must provide concurrency safety.
type ConnectionStore interface {
	// Update allows modification of the connection specified by the provided id.
	//
	// The connection returned from writeFunc is used to persist any changes. This applies
	// even if writeFunc also returns an error. Return a nil connection to signal that no
	// persistence should occur.
	//
	// This funcction only calls writeFunc if the connection exists. If no connection is found,
	// this operation returns an error.
	Update(id ID, writeFunc func(conn Connection) (Connection, error)) error
	// Read allows concurrent access to a connection but does not allow modification.
	//
	// This function does not ensure that the connection exists and calls readFunc regardless.
	// As such any nil checking must be performed in readFunc.
	Read(id ID, readFunc func(conn Connection) error) error
	Create(sender, receiver peer.Peer, status Status) (ID, error)
	Accept(id ID, sender, receiver peer.Peer, status Status) error
	All() ([]ID, error)
}

type Options struct {
	WitnessSelector WitnessSelector
}

type WitnessSelector interface {
	witnessSelector()
	String() string
}

type senderSelects struct{}
type receiverSelects struct{}

func (_ *senderSelects) witnessSelector()   {}
func (_ *receiverSelects) witnessSelector() {}

func (_ *senderSelects) String() string   { return "Sender" }
func (_ *receiverSelects) String() string { return "Receiver" }

var sendSelect WitnessSelector = &senderSelects{}
var recvSelect WitnessSelector = &receiverSelects{}

func WitnessSelectorSender() WitnessSelector   { return sendSelect }
func WitnessSelectorReceiver() WitnessSelector { return recvSelect }

func ParseWitnessSelector(s string) (WitnessSelector, error) {
	switch s {
	case sendSelect.String():
		return sendSelect, nil
	case recvSelect.String():
		return recvSelect, nil
	default:
		return nil, fmt.Errorf("Unrecognized witness selector type: %s", s)
	}
}

type ConnectRequest struct {
	ConnID   ID      `json:"connId"`
	Sender   peer.ID `json:"sender"`
	Receiver peer.ID `json:"receiver"`
	Options  Options `json:"options"`
}

type ConnectResponse struct {
	ConnID ID `json:"connId"`
	// Errors represent any errors that may have occurred during processing of the request
	// that the receiver would like to make the sender aware of.
	// This is distinct from the decision to not accept the connection for whatever business
	// logic reasons, which should be signaled using the Accept flag and RejectReasons.
	//
	// If Errors is both non-nil and non-empty, then the values of Accept and RejectReasons
	// are ignored.
	//
	// The underlying transport may use this to notify the sender of errors or may use some
	// other method to inform the sender-side transport layer that it should signal an error.
	Errors        []string `json:"errors"`
	Accepted      bool     `json:"accepted"`
	RejectReasons []string `json:"rejectReasons"`
}

type ConnectRejectError struct {
	ConnID  ID
	Reasons []string
}

func (e *ConnectRejectError) Error() string {
	return fmt.Sprintf("Connection %s connect request was rejected for the following reasons: %v", e.ConnID, e.Reasons)
}

type WitnessRequest struct {
	ConnID   ID      `json:"connId"`
	Sender   peer.ID `json:"sender"`
	Receiver peer.ID `json:"receiver"`
	Options  Options `json:"options"`
}

type WitnessResponse struct {
	ConnID ID `json:"connId"`
	// Errors represent any errors that may have occurred during processing of the request
	// that the receiver would like to make the sender aware of.
	// This is distinct from the decision to not accept the connection for whatever business
	// logic reasons, which should be signaled using the Accept flag and RejectReasons.
	//
	// If Errors is both non-nil and non-empty, then the values of Accept and RejectReasons
	// are ignored.
	//
	// The underlying transport may use this to notify the sender of errors or may use some
	// other method to inform the sender-side transport layer that it should signal an error.
	Errors        []string `json:"errors"`
	Accepted      bool     `json:"accepted"`
	RejectReasons []string `json:"rejectReasons"`
}

type WitnessRejectError struct {
	ConnID  ID
	Reasons []string
}

func (e *WitnessRejectError) Error() string {
	return fmt.Sprintf("Connection %s witness request was rejected for the following reasons: %v", e.ConnID, e.Reasons)
}

type CloseError struct {
	ConnID ID
	Errors []error
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("Errors encountered while closing connectin %s: %v", e.ConnID, e.Errors)
}

type CloseRequest struct {
	ConnID ID `json:"connId"`
}

func NewRandomID() (ID, error) {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return ID(uuid), err
	}
	return ID(uuid), nil
}

type WitnessNotification struct {
	ConnectionID ID      `json:"connectionId"`
	Witness      peer.ID `json:"witness"`
}
