package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"automationshutdown/internal/pve"
)

type fakePVE struct {
	statuses map[int]string
	stops    map[int]bool
}

func (f *fakePVE) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/cluster/resources"):
		parts := []map[string]any{
			{"vmid": 100, "name": "web", "node": "pve1", "type": "qemu", "status": "running", "tags": "praktik;kelas-a"},
			{"vmid": 101, "name": "db", "node": "pve1", "type": "qemu", "status": "running", "tags": "lain"},
			{"vmid": 102, "name": "off", "node": "pve1", "type": "qemu", "status": "stopped", "tags": "praktik"},
			{"vmid": 103, "name": "tmpl", "node": "pve1", "type": "qemu", "status": "running", "tags": "praktik", "template": 1},
			{"vmid": 200, "name": "ct", "node": "pve2", "type": "lxc", "status": "running", "tags": "praktik"},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": parts})
	case strings.HasSuffix(p, "/status/current"):
		vmid := vmidFromPath(p)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": f.statuses[vmid]}})
	case strings.HasSuffix(p, "/status/shutdown"):
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:00000001:00000001:00000001:qmshutdown:100:root@pam:"})
	case strings.HasSuffix(p, "/status/stop"):
		vmid := vmidFromPath(p)
		f.stops[vmid] = true
		f.statuses[vmid] = "stopped"
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:00000002:00000002:00000002:qmstop:100:root@pam:"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func vmidFromPath(p string) int {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segs {
		if (s == "qemu" || s == "lxc") && i+1 < len(segs) {
			n, _ := strconv.Atoi(segs[i+1])
			return n
		}
	}
	return 0
}

func testManager(t *testing.T, f *fakePVE) *Manager {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	c := pve.NewClient([]string{srv.URL}, "user@pam!id=secret", true)
	return NewManager(c, Options{ForceAfterS: 2, PollIntervalS: 1, InterDelayS: 0})
}

func waitDone(t *testing.T, j *Job) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if j.CurrentStatus() != "running" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
}

func hasEvent(j *Job, vmid int, event string) bool {
	for _, e := range j.History() {
		if e.VMID == vmid && e.Event == event {
			return true
		}
	}
	return false
}

func guest(vmid int, typ string) pve.Guest {
	return pve.Guest{VMID: vmid, Name: "g", Node: "pve1", Type: typ, Status: "running"}
}

func TestClusterVMParsing(t *testing.T) {
	f := &fakePVE{statuses: map[int]string{}, stops: map[int]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	c := pve.NewClient([]string{srv.URL}, "user@pam!id=secret", true)
	guests, err := c.ClusterVMs()
	if err != nil {
		t.Fatalf("ClusterVMs: %v", err)
	}
	if len(guests) != 5 {
		t.Fatalf("want 5 guests, got %d", len(guests))
	}
	if len(guests[0].Tags) != 2 || guests[0].Tags[0] != "praktik" {
		t.Fatalf("tags not split: %+v", guests[0].Tags)
	}
	if !guests[3].Template {
		t.Fatal("vmid 103 should be template")
	}
}

func TestGracefulStop(t *testing.T) {
	f := &fakePVE{statuses: map[int]string{100: "stopped"}, stops: map[int]bool{}}
	m := testManager(t, f)
	j, err := m.Start([]pve.Guest{guest(100, "qemu")}, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, j)
	if j.CurrentStatus() != "done" {
		t.Fatalf("status = %q", j.CurrentStatus())
	}
	if !hasEvent(j, 100, "stopped") {
		t.Fatal("missing stopped event")
	}
	if f.stops[100] {
		t.Fatal("force stop should not have been called")
	}
}

func TestForceStopAfterTimeout(t *testing.T) {
	f := &fakePVE{statuses: map[int]string{101: "running"}, stops: map[int]bool{}}
	m := testManager(t, f)
	j, err := m.Start([]pve.Guest{guest(101, "qemu")}, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, j)
	if !hasEvent(j, 101, "force-stop") {
		t.Fatal("missing force-stop event")
	}
	if !f.stops[101] {
		t.Fatal("force stop was not called")
	}
	if !hasEvent(j, 101, "stopped") {
		t.Fatal("missing stopped event after force stop")
	}
}

func TestDryRun(t *testing.T) {
	f := &fakePVE{statuses: map[int]string{100: "running"}, stops: map[int]bool{}}
	m := testManager(t, f)
	j, err := m.Start([]pve.Guest{guest(100, "qemu")}, true)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, j)
	if f.stops[100] {
		t.Fatal("dry-run must not call stop")
	}
	if f.statuses[100] != "running" {
		t.Fatal("dry-run must not change guest state")
	}
}

func TestSingleFlight(t *testing.T) {
	f := &fakePVE{statuses: map[int]string{100: "running"}, stops: map[int]bool{}}
	m := testManager(t, f)
	j1, err := m.Start([]pve.Guest{guest(100, "qemu")}, true)
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := m.Start([]pve.Guest{guest(100, "qemu")}, true); !IsRunningErr(err) {
		t.Fatalf("second Start should conflict, got %v", err)
	}
	m.Cancel(j1.ID)
	waitDone(t, j1)
	if j1.CurrentStatus() != "cancelled" {
		t.Fatalf("status = %q", j1.CurrentStatus())
	}
}
