package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-clarinet/config"
	"github.com/go-clarinet/log"
	"github.com/go-clarinet/p2p"
)

const initiateConnectionPath = "/admin/connect"
const addPeerPath = "/admin/peer"

func StartAdminServer(config *config.Config) error {
	http.HandleFunc(initiateConnectionPath, initiateConnection)
	http.HandleFunc(addPeerPath, addPeer)
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
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), fmt.Sprintf("Failed to add peer: %s\n", err.Error())})
		return
	}
	writeResponse(w, http.StatusOK, nil, nil)
}
