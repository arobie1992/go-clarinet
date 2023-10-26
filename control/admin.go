package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/p2p"
	"github.com/go-clarinet/repository"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

const initiateConnectionPath = "/admin/connect"
const addPeerPath = "/admin/peer"
const sendDataPath = "/admin/send"
const closeConnectionPath = "/admin/close"

func StartAdminServer(config *config.Config) error {
	http.HandleFunc(initiateConnectionPath, initiateConnection)
	http.HandleFunc(addPeerPath, addPeer)
	http.HandleFunc(sendDataPath, sendData)
	http.HandleFunc(closeConnectionPath, closeConnection)
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
	if err := requestConnection(req.TargetNode); err != nil {
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
	ConnID uuid.UUID
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

	if err = p2p.SendData(req.ConnID, req.NumBytes); err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to send data."})
		return
	}
	writeResponse(w, http.StatusOK, nil, nil)
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

	conn := p2p.Connection{ID: req.ConnID}
	if tx := repository.GetDB().Clauses(clause.Locking{Strength: "UPDATE"}).Find(&conn); tx.Error != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Failed to find connection."})
		return
	}
	defer repository.GetDB().Save(&conn)

	if conn.Sender != p2p.GetFullAddr() {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Currently only sender may close a connection."})
		return
	}

	if conn.Status != p2p.ConnectionStatusClosed {
		conn.Status = p2p.ConnectionStatusClosed
		if conn.Witness != "" {
			if err := sendCloseRequest(&conn, conn.Witness); err != nil {
				log.Log().Errorf("Close request for %s to witness failed: %s", conn.ID, err.Error())
			}
		}
		if err := sendCloseRequest(&conn, conn.Receiver); err != nil {
			log.Log().Errorf("Close request for %s to witness failed: %s", conn.ID, err.Error())
		}
	}

	writeResponse(w, http.StatusOK, nil, nil)
}