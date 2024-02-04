package peer

// ID is the unique identifier for the peer. If two instances of Peer have the same ID, they must be
// considered logically identical.
//
// ID is an interface to allow flexibility in display formatting, but all implementations must
// maintain the invariant that logically equivalent IDs conform to standard Go equality operations.
// For example, given some implementation type MyID and a create function NewMyID(string) where the
// string input determines logical uniqueness, NewMyID("test") == NewMyID("test") must be true.
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
	ID() ID
	// Addresses are the addresses at which the Peer is reachable, for example a URL or IP address and port.
	Addresses() []Address
}

type PeersRequest struct {
	Num int
}

type PeersResponse struct {
	Peers []Peer
}
