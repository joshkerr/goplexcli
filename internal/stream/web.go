package stream

import (
	"embed"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"time"
)

//go:embed templates/*
var templatesFS embed.FS

var templates *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"divf": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"float64": func(v int) float64 {
			return float64(v)
		},
		"urlEncode": func(s string) string {
			return url.QueryEscape(s)
		},
	}
	templates, err = template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
}

// WebHandler serves the web UI
func (s *Server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	streams := s.ListStreams()

	data := struct {
		Streams    []*StreamItem
		ServerName string
		Port       int
		Time       string
		LocalIP    string
	}{
		Streams:    streams,
		ServerName: s.hostname,
		Port:       s.port,
		Time:       time.Now().Format("15:04:05"),
		LocalIP:    getLocalIP(),
	}

	if err := templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "localhost"
}
