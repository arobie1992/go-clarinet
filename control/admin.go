package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"

	"github.com/arobie1992/go-clarinet/config"
	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/p2p"
	"github.com/arobie1992/go-clarinet/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const initiateConnectionPath = "/admin/connect"
const addPeerPath = "/admin/peer"
const sendDataPath = "/admin/send"
const closeConnectionPath = "/admin/close"
const queryPath = "/admin/query"
const requestPeersPath = "/admin/request-peers"

func StartAdminServer(config *config.Config) error {
	http.HandleFunc(initiateConnectionPath, initiateConnection)
	http.HandleFunc(addPeerPath, addPeer)
	http.HandleFunc(sendDataPath, sendData)
	http.HandleFunc(closeConnectionPath, closeConnection)
	http.HandleFunc(queryPath, query)
	http.HandleFunc(requestPeersPath, requestPeers)
	log.Log().Info("Starting http server")
	return http.ListenAndServe(fmt.Sprintf(":%d", config.Admin.Port), nil)
}

type adminConnectRequest struct {
	TargetNode string `json:"targetNode"`
}

type badResp struct {
	Err    string `json:"err"`
	Detail string `json:"detail"`
}

func initiateConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req adminConnectRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	log.Log().Infof("Received request to connect to %s", req.TargetNode)
	if _, err := RequestConnection(req.TargetNode); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to request connection."})
		return
	}
	writeResponse(w, http.StatusOK, nil, nil)
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
			log.Log().Errorf("Failed to serialize body: %s", err)
			// TODO better handling. This is just so the request completes at all.
			_, err = w.Write([]byte{})
			if err != nil {
				log.Log().Errorf("Failed to write body: %s", err)
			}
			return
		}
		_, err = w.Write(m)
		if err != nil {
			log.Log().Infof("Failed to write body: %s", err)
		}
	}
}

type addPeerRequest struct {
	PeerAddress string `json:"peerAddress"`
}

func addPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req addPeerRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	_, err = p2p.AddPeer(req.PeerAddress)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to add peer."})
		return
	}
	writeResponse(w, http.StatusOK, nil, nil)
}

type sendDataRequest struct {
	ConnID   uuid.UUID
	NumBytes int
}

func sendData(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req sendDataRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	if err = p2p.SendData(req.ConnID, []byte(makeRandomData(req.NumBytes))); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to send data."})
		return
	}
	writeResponse(w, http.StatusOK, nil, nil)
}

func makeRandomData(numBytes int) string {
	data := make([]byte, numBytes)
	for i := 0; i < numBytes; i++ {
		data[i] = byte(rand.Intn(128))
	}
	return string(data)
}

type closeConnectionRequest struct {
	ConnID uuid.UUID
}

func closeConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req closeConnectionRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	if err := CloseConnection(req.ConnID); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Error while closing connection."})
		return
	}

	writeResponse(w, http.StatusOK, nil, nil)
}

type queryRequest struct {
	ConnID uuid.UUID
	SeqNo  int
	// queryTarget is the node to query for the message. Valid values are "sender", "receiver", "witness". All are case-insensitive.
	QueryTarget string
}

func query(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req queryRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	connID := req.ConnID
	seqNo := req.SeqNo
	if seqNo == -1 {
		d, err := selectRandomMessage()
		if err != nil {
			writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to find random message."})
			return
		}
		connID = d.ConnID
		seqNo = d.SeqNo
	}

	conn := p2p.Connection{ID: connID}
	if tx := repository.GetDB().Find(&conn); tx.Error != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Error finding connection."})
		return
	}
	if conn.Status == -1 {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to find connection."})
		return
	}

	var nodeAddr string
	switch strings.ToLower(req.QueryTarget) {
	case "sender":
		nodeAddr = conn.Sender
	case "witness":
		nodeAddr = conn.Witness
	case "receiver":
		nodeAddr = conn.Receiver
	default:
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid query target."})
		return
	}

	if nodeAddr == p2p.GetFullAddr() {
		writeResponse(w, http.StatusBadRequest, nil, badResp{"", "Cannot query self."})
		return
	}

	if _, _, err := QueryForMessage(nodeAddr, conn, seqNo); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Error while sending query."})
		return
	}

	writeResponse(w, http.StatusOK, nil, nil)
}

func selectRandomMessage() (p2p.DataMessage, error) {
	d := p2p.DataMessage{ConnID: uuid.UUID{}, SeqNo: -1, SendSig: "", WitSig: ""}
	if tx := repository.GetDB().Clauses(clause.OrderBy{Expression: gorm.Expr("random()")}).First(&d); tx.Error != nil {
		return d, tx.Error
	}
	if d.SeqNo == -1 {
		return d, errors.New("Failed to find any data message.")
	}
	return d, nil
}

type requestPeersRequest struct {
	TargetNode string
	NumPeers   int
}

func requestPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var req requestPeersRequest
	err = json.Unmarshal(data, &req)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	if req.TargetNode == p2p.GetFullAddr() {
		writeResponse(w, http.StatusBadRequest, nil, badResp{"", "Cannot request peers from self."})
		return
	}

	if err := SendRequestPeersRequest(req.TargetNode, req.NumPeers); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Error while sending query."})
		return
	}

	writeResponse(w, http.StatusOK, nil, nil)
}
