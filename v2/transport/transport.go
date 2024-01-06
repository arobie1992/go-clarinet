package transport

import (
	"time"

	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/peer"
)

type Transport interface {
	Send(peer peer.Peer, options Options, data any) error
	Exchange(peer peer.Peer, options Options, data any, response any) (int, error)
	RegisterHandlers(
		connectHandler ConnectHandler,
		witnessHandler WitnessHandler,
		witnessNotificationHandler WitnessNotificationHandler,
		closeHandler CloseHandler,
	) error
}

// The options for configuring methods on Transport implementations. Nil values represent the default value for a given Transport implementation.
type Options struct {
	// Timeout is a generalized timeout that can be applied as desired by the underlying transport implementation.
	// For example, it may be applied per operation or treated as the overarching timeout for the entire operation.
	//
	// If a transport implementation uses this, it should specify how in the documentation for all of the Transport operations.
	Timeout           *time.Duration
	WriteTimeout      *time.Duration
	ReadTimeout       *time.Duration
	AdditionalOptions map[string]any
}

type SendHandler[I any] interface {
	// Handle allows access to the request for application processing for unidirectional
	// ccommunications.
	//
	// Returning an error indicates a desire to let the underlying transport notify the sender
	// using whatever means the implementation decides on. As this is logically unidirectional,
	// there is no guarantee transport implementations will support bidirectional communication.
	// However, it is strongly encouraged for them to do so whenever possible.
	Handle(peerID peer.ID, request I) error
	Options() Options
}

type ExchangeHandler[I, O any] interface {
	// Handle allows access to the request for application processing and delivers the output
	// to the sender.
	//
	// Returning an error indicates a desire to let the underlying transport notify the sender
	// using whatever means the implementation decides on. If you wish to notify the sender
	// of errors through an application level means, you must use those in your output type
	// and return a nil error.
	Handle(peerID peer.ID, request I) (O, error)
	Options() Options
}

type ConnectHandler = ExchangeHandler[connection.ConnectRequest, connection.ConnectResponse]
type WitnessHandler = ExchangeHandler[connection.WitnessRequest, connection.WitnessResponse]
type WitnessNotificationHandler = SendHandler[connection.WitnessNotification]
type CloseHandler = SendHandler[connection.CloseRequest]

type Handlers struct {
	ConnectHandler             ConnectHandler
	WitnessHandler             WitnessHandler
	WitnessNotificationHandler WitnessNotificationHandler
	CloseHandler               CloseHandler
}
