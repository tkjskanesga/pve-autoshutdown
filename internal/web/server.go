package web

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"automationshutdown/internal/config"
	"automationshutdown/internal/jobs"
	"automationshutdown/internal/pve"
	"automationshutdown/internal/session"
)

const sessionCookie = "as_session"

//go:embed all:dist
var distFS embed.FS

type Server struct {
	cfg  *config.Config
	pve  *pve.Client
	sess *session.Store
	jobs *jobs.Manager
	mux  *http.ServeMux
	ws   websocket.Upgrader
}

func New(cfg *config.Config) *Server {
	pveClient := pve.NewClient(cfg.PVEHosts, cfg.PVEToken, cfg.PVEVerifySSL)
	s := &Server{
		cfg:  cfg,
		pve:  pveClient,
		sess: session.NewStore(time.Duration(cfg.SessionTTLHrs) * time.Hour),
		jobs: jobs.NewManager(pveClient, jobs.Options{
			ForceAfterS:   cfg.ForceAfterS,
			PollIntervalS: cfg.PollIntervalS,
			InterDelayS:   cfg.InterVMDelayS,
		}),
		mux: http.NewServeMux(),
		ws: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		s.mux.ServeHTTP(rec, r)
		if !strings.HasPrefix(r.URL.Path, "/assets/") {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.code, time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	return h.Hijack()
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("POST /api/logout", s.logout)
	s.mux.HandleFunc("GET /api/me", s.authed(s.me))
	s.mux.HandleFunc("GET /api/vms/preview", s.authed(s.preview))
	s.mux.HandleFunc("POST /api/shutdown-all", s.authed(s.shutdownAll))
	s.mux.HandleFunc("GET /api/jobs/", s.authed(s.jobStatus))
	s.mux.HandleFunc("DELETE /api/jobs/", s.authed(s.jobCancel))
	s.mux.HandleFunc("GET /ws", s.authedWS(s.wsHandler))
	s.mux.HandleFunc("/", s.frontend)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.sess.Valid(c.Value) {
			writeErr(w, http.StatusUnauthorized, "login required")
			return
		}
		next(w, r)
	}
}

func (s *Server) authedWS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.sess.Valid(c.Value) {
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

type loginBody struct {
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var b loginBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.sess.Check(s.cfg.WebPassword, b.Password) {
		writeErr(w, http.StatusUnauthorized, "wrong password")
		return
	}
	tok := s.sess.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.cfg.SessionTTLHrs * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sess.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"dry_run":       s.cfg.DryRun,
		"tags_filter":   s.cfg.TargetTags,
	})
}

func matchTags(guestTags, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, t := range guestTags {
		set[t] = true
	}
	for _, f := range filter {
		if set[f] {
			return true
		}
	}
	return false
}

func (s *Server) buildTargets() ([]pve.Guest, error) {
	guests, err := s.pve.ClusterVMs()
	if err != nil {
		return nil, err
	}
	var out []pve.Guest
	for _, g := range guests {
		if g.Status != "running" || g.Template {
			continue
		}
		if s.cfg.ExcludeVMIDs[g.VMID] {
			continue
		}
		if !matchTags(g.Tags, s.cfg.TargetTags) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	targets, err := s.buildTargets()
	if err != nil {
		log.Printf("preview: %v", err)
		writeErr(w, http.StatusBadGateway, "proxmox unreachable: "+err.Error())
		return
	}
	if targets == nil {
		targets = []pve.Guest{}
	}
	log.Printf("preview: %d running targets (tags=%v)", len(targets), s.cfg.TargetTags)
	writeJSON(w, http.StatusOK, map[string]any{
		"total_running": len(targets),
		"targets":       targets,
		"tags_filter":   s.cfg.TargetTags,
		"dry_run":       s.cfg.DryRun,
	})
}

func (s *Server) shutdownAll(w http.ResponseWriter, r *http.Request) {
	targets, err := s.buildTargets()
	if err != nil {
		log.Printf("shutdown-all: %v", err)
		writeErr(w, http.StatusBadGateway, "proxmox unreachable: "+err.Error())
		return
	}
	j, err := s.jobs.Start(targets, s.cfg.DryRun)
	if err != nil {
		if jobs.IsRunningErr(err) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "total": j.Total, "dry_run": j.DryRun})
}

func (s *Server) jobID(r *http.Request) string {
	return strings.TrimPrefix(r.URL.Path, "/api/jobs/")
}

func (s *Server) jobStatus(w http.ResponseWriter, r *http.Request) {
	j := s.jobs.Get(s.jobID(r))
	if j == nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) jobCancel(w http.ResponseWriter, r *http.Request) {
	if !s.jobs.Cancel(s.jobID(r)) {
		writeErr(w, http.StatusNotFound, "job not found or already finished")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func (s *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("jobId")
	j := s.jobs.Get(id)
	if j == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	conn, err := s.ws.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for range t.C {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()
	sent := 0
	for {
		hist := j.History()
		for ; sent < len(hist); sent++ {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(hist[sent]); err != nil {
				return
			}
		}
		if j.CurrentStatus() != "running" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws") {
		http.NotFound(w, r)
		return
	}
	if s.cfg.FrontendMode == "proxy" {
		target, err := url.Parse(s.cfg.ViteDevURL)
		if err != nil {
			http.Error(w, "bad VITE_DEV_URL", http.StatusInternalServerError)
			return
		}
		httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
		return
	}
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(sub, path); err != nil {
		path = "index.html"
	}
	if _, err := fs.Stat(sub, path); err != nil {
		http.Error(w, "frontend not built yet, run the frontend build first", http.StatusServiceUnavailable)
		return
	}
	http.ServeFileFS(w, r, sub, path)
}
