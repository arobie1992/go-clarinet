package reputation

import "github.com/arobie1992/go-clarinet/v2/peer"

type Reputation interface {
	PeerID() peer.ID
	Value() float64
	StrongPenalize() Reputation
	WeakPenalize() Reputation
	Reward() Reputation
}

// ReputationStore represents some sort of backing storage for the reputation information about a node's peers.
//
// All nodes are considered having a reputation at all times. This means that all operations on a reputation store
// must always pass a meaningful value to the readFunc or writeFunc. Implementations are free to approach this however
// they please.
type ReputationStore interface {
	// Update allows modification of the reputation corresponding to the provided peer.
	//
	// The reputation returned from writeFunc is used to persist any changes. This applies
	// even if writeFunc also returns an error. Return a nil reputation to signal that no
	// persistence should occur.
	Update(peerID peer.ID, writeFunc func(rep Reputation) (Reputation, error)) error
	Read(peerID peer.ID, readFunc func(rep Reputation) error) error
}

type TrustFunction = func(rep Reputation) (bool, error)
