package p2p

import (
	"strings"
	"time"

	"github.com/arobie1992/go-clarinet/log"
	"github.com/libp2p/go-libp2p/core/network"
)

// These can result in hanging indefinitely if it keeps failing, but that's basically the state the other end
// is in at the moment so will cross that bridge during cleanup. Ideally would probably have the other end have
// a timeout for their receives as well.

func EnsureClose(s network.Stream) {
	otherSide := getSender(s)
	for i, err := 1, s.Close(); err != nil; i, err = i+1, s.Close() {
		// This is a terrible fix; actually figure out what this means and handle it properly
		if strings.Contains(err.Error(), "close called for canceled stream") {
			break;
		}
		log.Log().Errorf("Failed to close stream to/from %s. Error: %s. Sleep for %d seconds and retry.", otherSide, err, i)
		time.Sleep(time.Duration(i) * time.Second)
	}
}

func EnsureReset(s network.Stream) {
	otherSide := getSender(s)
	for i, err := 1, s.Reset(); err != nil; i, err = i+1, s.Reset() {
		// This is a terrible fix; actually figure out what this means and handle it properly
		if strings.Contains(err.Error(), "close called for canceled stream") {
			break;
		}
		log.Log().Errorf("Failed to reset stream to/from %s. Error: %s. Sleep for %d seconds and retry.", otherSide, err, i)
		time.Sleep(time.Duration(i) * time.Second)
	}
}
