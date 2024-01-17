package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	golog "log"
	"math"
	"net"
	"net/http"
	"os"

	lcrypto "github.com/libp2p/go-libp2p/core/crypto"

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
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
)

func die(l log.Logger, message string, args ...any) {
	l.Error(message, args...)
	os.Exit(1)
}

type config struct {
	Libp2pPort int
	CertPath   string
	AdminPort  int
	LogLevel   string
	Host       string
}

func loadConfig() config {
	if len(os.Args) != 2 {
		golog.Fatalln("Please provide a config.")
	}
	cfgPath := os.Args[1]
	contents, err := os.ReadFile(cfgPath)
	if err != nil {
		golog.Fatalf("Failed to read config: %s\n", err)
	}
	var cfg config
	err = json.Unmarshal(contents, &cfg)
	if err != nil {
		golog.Fatalf("Failed to parse config: %s\n", err)
	}
	return cfg
}

func initLogger(cfg config) log.Logger {
	level, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		golog.Fatalf("Failed to parse log level: %s\n", err)
	}
	logger, err := zap.NewZapLogger(level)
	if err != nil {
		golog.Fatalf("Failed to initialize logger: %s\n", err)
	}
	return logger
}

func loadPrivKey(file string) (lcrypto.PrivKey, error) {
	r, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("Invalid key format")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("Provided Key was neither PKCS1 nor PKCS8. Please use one of those formats.")
		}
	}

	pk, _, err := crypto.KeyPairFromStdKey(key)

	return pk, nil
}

func initLibp2pHost(logger log.Logger, cfg config) host.Host {
	hostIP := cfg.Host
	if hostIP == "" {
		hostname, err := os.Hostname()
		if err != nil {
			die(logger, "Failed to find computer hostname: %s", err)
		}
		addrs, err := net.LookupHost(hostname)
		if err != nil {
			die(logger, "Failed to find addresses for hostname: %s", err)
		}
		for _, addr := range addrs {
			if net.ParseIP(addr).IsLoopback() {
				continue
			}
			hostIP = addr
			break
		}
	}
	addr := fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", hostIP, cfg.Libp2pPort)

	var err error
	var host host.Host
	if cfg.CertPath != "" {
		var priv crypto.PrivKey
		priv, err = loadPrivKey(cfg.CertPath)
		if err != nil {
			die(logger, "Failed to load private key: %s", err)
		}
		host, err = lp2p.New(lp2p.ListenAddrStrings(addr), lp2p.Identity(priv), lp2p.DisableRelay())
	} else {
		host, err = lp2p.New(lp2p.ListenAddrStrings(addr), lp2p.DisableRelay())
	}
	if err != nil {
		die(logger, "Failed to create libp2p host: %s", err)
	}

	logger.Info("Created libp2p host with ID %s and addresses %v", host.ID(), host.Addrs())
	return host
}

func main() {
	cfg := loadConfig()
	logger := initLogger(cfg)
	host := initLibp2pHost(logger, cfg)
	peerstore := libp2p.NewPeerStore(host, logger)
	trpt := libp2p.NewTransport(host, logger)
	connectionstore := inmem.NewConnectionStore(logger)
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
			err := reputationstoree.Read(p.ID(), func(rep reputation.Reputation) error {
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
		die(logger, "Got error while creating node: %s", err)
	}

	self, err := node.Self()
	if err != nil {
		die(logger, "Got error while attempting to retrieve self: %s", err)
	}
	logger.Info("I am %s and I have addresses %v", self.ID(), self.Addresses())

	startHttpServer(node, logger, &cfg)
}

type connectHandler struct {
	options transport.Options
}

func (h *connectHandler) Handle(peerID peer.ID, request connection.ConnectRequest) (connection.ConnectResponse, error) {
	return connection.ConnectResponse{
		ConnectionID:  request.ConnID,
		Errors:        []string{},
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
		ConnectionID:  request.ConnID,
		Errors:        []string{},
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

func startHttpServer(n *v2.Node, logger log.Logger, cfg *config) {
	http.HandleFunc("/connect", adapt(req{}, &connectHttpHandler{n}))
	http.HandleFunc("/peer", adapt(req{}, &updatePeerHandler{n}))
	http.HandleFunc("/peers", getAdapt(req{}, &listPeerHandler{n, logger}))
	http.HandleFunc("/connections", getAdapt(req{}, &listConnectionsHandler{n, logger}))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.AdminPort), nil); err != nil {
		panic(fmt.Sprintf("Failed to start http server: %s", err))
	}
}

type connectHttpHandler struct {
	n *v2.Node
}

func (h *connectHttpHandler) handle(r req) (map[string]string, error) {
	id, err := libp2p.ParsePeerID(r.PeerID)
	if err != nil {
		return nil, err
	}
	addrs := []peer.Address{}
	for _, a := range r.Addresses {
		addrs = append(addrs, peer.Address(a))
	}
	p := libp2p.NewPeer(id, addrs)
	connID, err := h.n.Connect(p, connection.Options{WitnessSelector: connection.WitnessSelectorSender()}, transport.Options{})
	if err != nil {
		return nil, err
	}
	return map[string]string{"connectionId": fmt.Sprintf("%s", connID)}, nil
}

type updatePeerHandler struct {
	n *v2.Node
}

func (h *updatePeerHandler) handle(r req) (map[string]string, error) {
	id, err := libp2p.ParsePeerID(r.PeerID)
	if err != nil {
		return nil, err
	}
	addresses := []peer.Address{}
	for _, a := range r.Addresses {
		addresses = append(addresses, peer.Address(a))
	}
	if err := h.n.UpdatePeer(libp2p.NewPeer(id, addresses)); err != nil {
		return nil, err
	}
	return map[string]string{"message": fmt.Sprintf("Successfully added peer %s with addresses %v", r.PeerID, r.Addresses)}, nil
}

type listPeerHandler struct {
	n   *v2.Node
	log log.Logger
}

func (h *listPeerHandler) handle(_ req) (map[string][]peerResp, error) {
	peers, err := h.n.Peers()
	if err != nil {
		return nil, err
	}
	mappedPeers := []peerResp{}
	for _, p := range peers {
		mappedPeers = append(mappedPeers, peerResp{p.ID(), p.Addresses()})
	}
	return map[string][]peerResp{"peers": mappedPeers}, nil
}

type peerResp struct {
	ID        peer.ID        `json:"id"`
	Addresses []peer.Address `json:"addresses"`
}

type listConnectionsHandler struct {
	n *v2.Node
	l log.Logger
}

func (h *listConnectionsHandler) handle(_ req) (map[string][]connResp, error) {
	connections, err := h.n.Connections()
	h.l.Trace("Found connections %v", connections)
	if err != nil {
		return nil, err
	}
	connResps := []connResp{}
	for _, c := range connections {
		h.l.Trace("Converting connection %v to readonly conn", c)
		id := fmt.Sprintf("%s", c.ID())
		sender := fmt.Sprintf("%s", c.Sender())
		witness := fmt.Sprintf("%s", c.Witness())
		receiver := fmt.Sprintf("%s", c.Receiver())
		status := fmt.Sprintf("%s", c.Status())
		connResps = append(connResps, connResp{id, sender, witness, receiver, status})
	}
	return map[string][]connResp{"connections": connResps}, nil
}

type connResp struct {
	ID       string
	Sender   string
	Witness  string
	Receiver string
	Status   string
}

type handler[I, O any] interface {
	handle(I) (O, error)
}

func writeResponse(w http.ResponseWriter, status int, headers map[string]string, jsonBody interface{}) {
	if headers != nil {
		for k, v := range headers {
			w.Header().Add(k, v)
		}
	}
	if jsonBody != nil {
		w.Header().Add("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if jsonBody != nil {
		m, err := json.Marshal(jsonBody)
		if err != nil {
			fmt.Printf("Failed to serialize body: %s", err)
			// TODO better handling. This is just so the request completes at all.
			_, err = w.Write([]byte{})
			if err != nil {
				fmt.Printf("Failed to write body: %s", err)
			}
			return
		}
		_, err = w.Write(m)
		if err != nil {
			fmt.Printf("Failed to write body: %s", err)
		}
	}
}

func adapt[I, O any](reqType I, h handler[I, O]) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeResponse(w, http.StatusInternalServerError, nil, errResp{Message: "Failed to read request body.", Error: err.Error()})
			return
		}

		if err := json.Unmarshal(body, &reqType); err != nil {
			writeResponse(w, http.StatusBadRequest, nil, errResp{Message: "Unable to parse request as JSON.", Error: err.Error()})
			return
		}

		handleAndReply(w, reqType, h)
	}
}

func handleAndReply[I, O any](w http.ResponseWriter, reqType I, h handler[I, O]) {
	resp, err := h.handle(reqType)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, errResp{Message: "Error during processing.", Error: err.Error()})
		return
	}

	jsonResp, err := json.Marshal(resp)
	if err != nil {
		fmt.Printf("Resp that failed to marshal: %v", resp)
		writeResponse(w, http.StatusInternalServerError, nil, errResp{Message: "Failed to serialize response as json.", Error: err.Error()})
		return
	}

	i, err := w.Write(jsonResp)
	if err != nil {
		fmt.Printf("Encountered error while writing response %s: %s", jsonResp, err)
		return
	}
	if i != len(jsonResp) {
		fmt.Printf("Did not write all bytes. Had: %d, Wrote: %d", len(jsonResp), i)
	}
}

func getAdapt[I, O any](unused I, h handler[I, O]) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "GET"}, nil)
			return
		}
		handleAndReply(w, unused, h)
	}
}

type req struct {
	PeerID    string
	Addresses []string
}

type errResp struct {
	Message string
	Error   string
}
