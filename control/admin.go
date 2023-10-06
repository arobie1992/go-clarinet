package control

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

const InitiateConnectionPath = "/admin/connect"

type connectReq struct {
	TargetNode string `json:"targetNode"`
}

type badResp struct {
	Err    string `json:"err"`
	Detail string `json:"detail"`
}

func InitiateConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeResponse(w, http.StatusMethodNotAllowed, map[string]string{"Allow": "POST"}, nil)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, nil, badResp{err.Error(), "Failed to read request body."})
		return
	}

	var cr connectReq
	err = json.Unmarshal(data, &cr)
	if err != nil {
		writeResponse(w, http.StatusUnprocessableEntity, nil, badResp{err.Error(), "Invalid body format."})
		return
	}

	log.Printf("Received request to connect to %s\n", cr.TargetNode)
	if err := requestConnection(cr.TargetNode); err != nil {
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
			log.Printf("Failed to serialize body: %s\n", err)
			return
		}
		_, err = w.Write(m)
		if err != nil {
			log.Printf("Failed to write body: %s\n", err)
		}
	}
}
