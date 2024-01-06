package main

import (
	"fmt"
	golog "log"
	"math"
	"net"
	"os"

	"github.com/arobie1992/go-clarinet/starterimpl/inmem"
	"github.com/arobie1992/go-clarinet/starterimpl/libp2p"
	"github.com/arobie1992/go-clarinet/starterimpl/zap"
	v2 "github.com/arobie1992/go-clarinet/v2"
	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
	"github.com/arobie1992/go-clarinet/v2/transport"

	lp2p "github.com/libp2p/go-libp2p"
)

func main() {
	logger, err := zap.NewZapLogger(log.Info())
	if err != nil {
		golog.Fatalf("Failed to initialize logger: %s\n", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		logger.Error("Failed to find computer hostname: %s", err)
		os.Exit(1)
	}
	addrs, err := net.LookupHost(hostname)
	if err != nil {
		logger.Error("Failed to find addresses for hostname: %s", err)
		os.Exit(1)
	}
	var hostIP string
	for _, addr := range addrs {
		if net.ParseIP(addr).IsLoopback() {
			continue
		}
		hostIP = addr
		break
	}
	addr := fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", hostIP, 9000)
	host, err := lp2p.New(lp2p.ListenAddrStrings(addr), lp2p.DisableRelay())
	if err != nil {
		logger.Error("Failed to create libp2p host: %s", err)
		os.Exit(1)
	}
	logger.Info("Created libp2p host with ID %s and addresses %v", host.ID(), host.Addrs())

	peerstore := libp2p.NewPeerStore(host)
	trpt := libp2p.NewTransport(host)
	connectionstore := inmem.NewConnectionStore()
	messagestore := inmem.NewMessageStore()
	reputationstoree := inmem.NewReputationStore(nil)

	trustFunc := func(rep reputation.Reputation) (bool, error) {
		if rep.Value() < 0.5 {
			return false, nil
		}

		peers, err := peerstore.All()
		if err != nil {
			return false, err
		}

		repVals := []float64{}
		for _, p := range peers {
			err := reputationstoree.ReadOperation(p.ID(), func(rep reputation.Reputation) error {
				repVals = append(repVals, rep.Value())
				return nil
			})
			if err != nil {
				logger.Error("Encountered error while getting reputation value for peer %s: %s", p.ID(), err)
			}
		}

		mean := float64(0)
		for _, rv := range repVals {
			mean += rv
		}
		mean /= float64(len(repVals))

		variance := float64(0)
		for _, rv := range repVals {
			variance += math.Pow(rv-mean, 2)
		}
		variance /= float64(len(repVals) - 1)

		stdev := math.Sqrt(variance)
		return rep.Value() >= (mean - stdev), nil
	}

	node, err := v2.NewNode(
		peerstore,
		connectionstore,
		messagestore,
		reputationstoree,
		trustFunc,
		trpt,
		transport.Handlers{
			ConnectHandler:             &connectHandler{},
			WitnessHandler:             &witnessHandler{},
			WitnessNotificationHandler: &witnessNotificationHandler{},
			CloseHandler:               &closeHandler{},
		},
		logger,
	)

	if err != nil {
		logger.Error("Got error while creating node: %s", err)
		os.Exit(1)
	}

	self, err := node.Self()
	if err != nil {
		logger.Error("Got error while attempting to retrieve self: %s", err)
		os.Exit(1)
	}
	logger.Info("I am %s and I have addresses %v", self.ID(), self.Addresses())
}

type connectHandler struct {
	options transport.Options
}

func (h *connectHandler) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	return connection.ConnectResponse{
		ConnID:        request.ConnID,
		Errors:        []error{},
		Accepted:      true,
		RejectReasons: []string{},
	}, nil
}

func (h *connectHandler) Options() transport.Options {
	return h.options
}

type witnessHandler struct {
	options transport.Options
}

func (h *witnessHandler) Handle(peerID peer.ID, request connection.WitnessRequest) (connection.WitnessResponse, error) {
	return connection.WitnessResponse{
		ConnID:        request.ConnID,
		Errors:        []error{},
		Accepted:      true,
		RejectReasons: []string{},
	}, nil
}

func (h *witnessHandler) Options() transport.Options {
	return h.options
}

type witnessNotificationHandler struct {
	options transport.Options
}

func (h *witnessNotificationHandler) Handle(peerID peer.ID, notification connection.WitnessNotification) error {
	return nil
}

func (h *witnessNotificationHandler) Options() transport.Options {
	return h.options
}

type closeHandler struct {
	options transport.Options
}

func (h *closeHandler) Handle(peerID peer.ID, request connection.CloseRequest) error {
	return nil
}

func (h *closeHandler) Options() transport.Options {
	return h.options
}
