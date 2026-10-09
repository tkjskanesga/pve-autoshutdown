package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strings"
	"sync"
	"time"

	"automationshutdown/internal/pve"
)

type Event struct {
	TS      time.Time `json:"ts"`
	JobID   string    `json:"job_id"`
	VMID    int       `json:"vmid,omitempty"`
	Node    string    `json:"node,omitempty"`
	Type    string    `json:"type,omitempty"`
	Event   string    `json:"event"`
	Message string    `json:"message"`
}

type Job struct {
	ID      string    `json:"job_id"`
	Status  string    `json:"status"`
	Total   int       `json:"total"`
	Done    int       `json:"done"`
	DryRun  bool      `json:"dry_run"`
	Started time.Time `json:"started"`

	mu     sync.Mutex
	events []Event
	cancel context.CancelFunc
	doneCh chan struct{}
}

func (j *Job) emit(vmid int, node, typ, event, msg string) {
	j.mu.Lock()
	j.events = append(j.events, Event{
		TS: time.Now(), JobID: j.ID, VMID: vmid,
		Node: node, Type: typ, Event: event, Message: msg,
	})
	j.mu.Unlock()
}

func (j *Job) History() []Event {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Event, len(j.events))
	copy(out, j.events)
	return out
}

func (j *Job) setStatus(s string) {
	j.mu.Lock()
	j.Status = s
	j.mu.Unlock()
}

func (j *Job) CurrentStatus() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status
}

func (j *Job) bumpDone() {
	j.mu.Lock()
	j.Done++
	j.mu.Unlock()
}

type Options struct {
	ForceAfterS   int
	PollIntervalS int
	InterDelayS   int
}

type Manager struct {
	mu   sync.Mutex
	jobs map[string]*Job
	pve  *pve.Client
	opt  Options
}

func NewManager(pveClient *pve.Client, opt Options) *Manager {
	return &Manager{jobs: map[string]*Job{}, pve: pveClient, opt: opt}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (m *Manager) Start(targets []pve.Guest, dryRun bool) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		j.mu.Lock()
		running := j.Status == "running"
		j.mu.Unlock()
		if running {
			return nil, errJobRunning
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{
		ID: newID(), Status: "running", Total: len(targets),
		DryRun: dryRun, Started: time.Now(),
		cancel: cancel, doneCh: make(chan struct{}),
	}
	m.jobs[j.ID] = j
	go m.run(ctx, j, targets)
	return j, nil
}

type runningError struct{}

func (runningError) Error() string { return "another shutdown job is already running" }

var errJobRunning = runningError{}

func IsRunningErr(err error) bool { return err == errJobRunning }

func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	j.mu.Lock()
	running := j.Status == "running"
	j.mu.Unlock()
	if !running {
		return false
	}
	j.cancel()
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (m *Manager) run(ctx context.Context, j *Job, targets []pve.Guest) {
	defer close(j.doneCh)
	names := make([]string, 0, len(targets))
	for _, g := range targets {
		names = append(names, itoa(g.VMID)+"("+g.Node+")")
	}
	log.Printf("job %s started: %d targets [%s] dry-run=%v", j.ID, len(targets), strings.Join(names, ","), j.DryRun)
	j.emit(0, "", "", "started", startMsg(j.DryRun, names))

	type tracked struct {
		g          pve.Guest
		shutdownAt time.Time
		forced     bool
		forceAt    time.Time
		finished   bool
		lastWait   time.Time
	}
	pending := make([]*tracked, 0, len(targets))

	for _, g := range targets {
		if ctx.Err() != nil {
			j.emit(g.VMID, g.Node, g.Type, "skipped", "job cancelled before dispatch, skipped")
			j.bumpDone()
			continue
		}
		if j.DryRun {
			j.emit(g.VMID, g.Node, g.Type, "shutdown-sent", "dry-run: would send graceful shutdown to "+g.Type+"/"+itoa(g.VMID)+" on "+g.Node)
			j.emit(g.VMID, g.Node, g.Type, "stopped", "dry-run: "+g.Type+"/"+itoa(g.VMID)+" marked stopped (no action taken)")
			j.bumpDone()
			continue
		}
		if err := m.pve.Shutdown(g.Node, g.Type, g.VMID, m.opt.ForceAfterS); err != nil {
			j.emit(g.VMID, g.Node, g.Type, "error", "shutdown request failed for "+g.Type+"/"+itoa(g.VMID)+": "+err.Error()+", still watching it")
		} else {
			j.emit(g.VMID, g.Node, g.Type, "shutdown-sent", "graceful shutdown sent to "+g.Type+"/"+itoa(g.VMID)+" on "+g.Node)
		}
		pending = append(pending, &tracked{g: g, shutdownAt: time.Now()})
		j.emit(g.VMID, g.Node, g.Type, "delay", "continuing to next guest without waiting for this one to stop")
		sleepCtx(ctx, time.Duration(m.opt.InterDelayS)*time.Second)
	}

	tick := time.Duration(m.opt.PollIntervalS) * time.Second
	lastSummary := time.Now()
	for {
		allDone := true
		for _, t := range pending {
			if t.finished {
				continue
			}
			allDone = false
			break
		}
		if allDone || ctx.Err() != nil {
			break
		}
		for _, t := range pending {
			if t.finished || ctx.Err() != nil {
				continue
			}
			g := t.g
			st, err := m.pve.GuestStatus(g.Node, g.Type, g.VMID)
			if err != nil {
				continue
			}
			if st != "running" {
				suffix := ""
				if t.forced {
					suffix = " after force stop"
				}
				j.emit(g.VMID, g.Node, g.Type, "stopped", g.Type+"/"+itoa(g.VMID)+" is now "+st+suffix)
				t.finished = true
				j.bumpDone()
				continue
			}
			now := time.Now()
			if !t.forced && now.After(t.shutdownAt.Add(time.Duration(m.opt.ForceAfterS)*time.Second)) {
				j.emit(g.VMID, g.Node, g.Type, "force-stop", g.Type+"/"+itoa(g.VMID)+" did not stop in time, forcing stop")
				log.Printf("job %s: force-stopping %d (%s)", j.ID, g.VMID, g.Node)
				if err := m.pve.Stop(g.Node, g.Type, g.VMID); err != nil {
					j.emit(g.VMID, g.Node, g.Type, "error", "force stop failed for "+g.Type+"/"+itoa(g.VMID)+": "+err.Error())
					t.finished = true
					j.bumpDone()
					continue
				}
				t.forced = true
				t.forceAt = now
				continue
			}
			if t.forced && now.After(t.forceAt.Add(60*time.Second)) {
				j.emit(g.VMID, g.Node, g.Type, "error", g.Type+"/"+itoa(g.VMID)+" still running after force stop, needs manual check")
				log.Printf("job %s: %d still running after force stop", j.ID, g.VMID)
				t.finished = true
				j.bumpDone()
				continue
			}
			if now.After(t.lastWait.Add(60 * time.Second)) {
				t.lastWait = now
				j.emit(g.VMID, g.Node, g.Type, "still-running", g.Type+"/"+itoa(g.VMID)+" still running, waiting")
			}
		}
		if time.Since(lastSummary) >= 60*time.Second {
			lastSummary = time.Now()
			left := 0
			for _, t := range pending {
				if !t.finished {
					left++
				}
			}
			j.emit(0, "", "", "still-running", "waiting for "+itoa(left)+" guest(s) to stop")
		}
		sleepCtx(ctx, tick)
	}
	for _, t := range pending {
		if !t.finished {
			j.emit(t.g.VMID, t.g.Node, t.g.Type, "skipped", "job cancelled, guest left as-is")
			j.bumpDone()
		}
	}
	if ctx.Err() != nil {
		j.setStatus("cancelled")
		j.emit(0, "", "", "cancelled", "job cancelled by operator")
		log.Printf("job %s cancelled", j.ID)
		return
	}
	j.setStatus("done")
	j.emit(0, "", "", "done", "all targets processed")
	log.Printf("job %s done", j.ID)
}

func startMsg(dry bool, names []string) string {
	list := strings.Join(names, ", ")
	if dry {
		return "dry-run started, no guest will actually be powered off. targets: " + list
	}
	return "shutdown started. targets: " + list
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
