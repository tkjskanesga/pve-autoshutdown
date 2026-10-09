package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"automationshutdown/internal/config"
)

func fakeProxmox() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/cluster/resources"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"vmid": 100, "name": "web", "node": "pve1", "type": "qemu", "status": "running", "tags": "praktik"},
				{"vmid": 101, "name": "db", "node": "pve1", "type": "qemu", "status": "running", "tags": "praktik"},
			}})
		case strings.HasSuffix(p, "/status/current"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "stopped"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:x"})
		}
	}))
}

func testServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	pve := fakeProxmox()
	t.Cleanup(pve.Close)
	t.Setenv("WEB_PASSWORD", "secret")
	t.Setenv("PVE_HOSTS", pve.URL)
	t.Setenv("PVE_TOKEN", "u@pam!id=s")
	t.Setenv("TARGET_TAGS", "praktik")
	t.Setenv("DRY_RUN", "false")
	t.Setenv("INTER_VM_DELAY_SEC", "0")
	t.Setenv("FORCE_AFTER_SEC", "60")
	t.Setenv("POLL_INTERVAL_SEC", "1")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv := httptest.NewServer(New(cfg).Handler())
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func login(t *testing.T, srv *httptest.Server, c *http.Client) string {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("login: %v status=%v", err, res.StatusCode)
	}
	defer res.Body.Close()
	for _, ck := range res.Cookies() {
		if ck.Name == sessionCookie {
			return ck.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestFullFlow(t *testing.T) {
	srv, c := testServer(t)
	tok := login(t, srv, c)

	withAuth := func(req *http.Request) *http.Response {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		res, err := c.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", req.URL.Path, err)
		}
		return res
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/vms/preview", nil)
	res := withAuth(req)
	var prev struct {
		Total int `json:"total_running"`
	}
	if err := json.NewDecoder(res.Body).Decode(&prev); err != nil {
		t.Fatalf("preview decode: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || prev.Total != 2 {
		t.Fatalf("preview: status=%d total=%d", res.StatusCode, prev.Total)
	}

	req, _ = http.NewRequest("POST", srv.URL+"/api/shutdown-all", nil)
	res = withAuth(req)
	var started struct {
		JobID string `json:"job_id"`
		Total int    `json:"total"`
	}
	if err := json.NewDecoder(res.Body).Decode(&started); err != nil {
		t.Fatalf("shutdown-all decode: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 202 || started.Total != 2 {
		t.Fatalf("shutdown-all: status=%d total=%d", res.StatusCode, started.Total)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?jobId=" + started.JobID
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Add("Cookie", sessionCookie+"="+tok)
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	sawDone := false
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var ev map[string]any
		if err := conn.ReadJSON(&ev); err != nil {
			break
		}
		if ev["event"] == "done" {
			sawDone = true
			break
		}
	}
	if !sawDone {
		t.Fatal("ws stream never delivered done event")
	}
}

func TestAuthRequired(t *testing.T) {
	srv, c := testServer(t)
	for _, p := range []string{"/api/me", "/api/vms/preview"} {
		req, _ := http.NewRequest("GET", srv.URL+p, nil)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: want 401, got %d", p, res.StatusCode)
		}
	}
}
