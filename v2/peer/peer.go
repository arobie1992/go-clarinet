package peer

type ID interface {
	String() string
}
type Address string

type PeerStore interface {
	AddAddr(id ID, addr Address) error
	// Find retrieves a peer with the give id from the underlying peerstore. If error != nil, whatever Peer value
	// is returned must be discarded.
	Find(id ID) (Peer, error)
	// All provides access to all peers currently in the peerstore.
	All() ([]Peer, error)
	Self() (Peer, error)
}

type Peer interface {
	// ID is the unique identifier for the peer. If two instances of Peer have the same ID, they must be
	// considered logically identical.
	ID() ID
	// Addresses are the addresses at which the Peer is reachable, for example a URL or IP address and port.
	Addresses() []Address
}
