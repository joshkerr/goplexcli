package dlserver

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
)

// Protocol is the wire-protocol version reported by /ping, so clients can
// detect a daemon too old or too new to talk to.
const Protocol = 1

// DefaultListen is the daemon's default listen address. 47821 sits next to
// lansync's 47820 in the same "goplexcli" range.
const DefaultListen = ":47821"

// PingResponse identifies a daemon to a probing client. Served without auth so
// the GUI can distinguish "offline" from "wrong token".
type PingResponse struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"` // runtime.GOOS
	Protocol int    `json:"protocol"`
}

// Server is the daemon's HTTP API: a thin authenticated shell around Manager.
//
// Auth is a single shared bearer token over plain HTTP — a LAN trust model,
// like qbt-dl's daemon. The token gates every endpoint except /ping.
type Server struct {
	name    string
	version string
	token   string
	mgr     *Manager
}

// New creates a Server for the given Manager. name identifies this machine in
// clients' pickers; version is the goplexcli build version.
func New(name, version, token string, mgr *Manager) *Server {
	return &Server{name: name, version: version, token: token, mgr: mgr}
}

// Manager returns the job manager (for shutdown wiring).
func (s *Server) Manager() *Manager { return s.mgr }

// Handler returns the daemon's route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ping", s.handlePing)
	mux.Handle("GET /api/v1/downloads", s.auth(s.handleList))
	mux.Handle("POST /api/v1/downloads", s.auth(s.handleSubmit))
	mux.Handle("POST /api/v1/downloads/{id}/cancel", s.auth(s.handleCancel))
	mux.Handle("POST /api/v1/downloads/clear-finished", s.auth(s.handleClearFinished))
	return mux
}

// auth wraps a handler with constant-time bearer-token verification.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid or missing bearer token")
			return
		}
		next(w, r)
	})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, PingResponse{
		Name:     s.name,
		Version:  s.version,
		Platform: runtime.GOOS,
		Protocol: Protocol,
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mgr.List())
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var reqs []DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(reqs) == 0 {
		writeError(w, http.StatusBadRequest, "no downloads in request")
		return
	}
	res, err := s.mgr.Submit(reqs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.Cancel(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearFinished(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.ClearFinished(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// LanURLs returns the http://<ip>:<port> URLs this daemon is reachable at on
// the LAN, one per non-loopback IPv4 interface, for the startup banner —
// exactly what a user pastes into the GUI's remote-server settings.
func LanURLs(listen string) []string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var urls []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			urls = append(urls, fmt.Sprintf("http://%s:%s", ip4, port))
		}
	}
	return urls
}
