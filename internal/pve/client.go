package pve

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Guest struct {
	VMID     int      `json:"vmid"`
	Name     string   `json:"name"`
	Node     string   `json:"node"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	Template bool     `json:"template"`
}

type envelope struct {
	Data json.RawMessage `json:"data"`
}

type Client struct {
	hosts []string
	token string
	http  *http.Client
}

func NewClient(hosts []string, token string, verifySSL bool) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySSL},
	}
	return &Client{
		hosts: hosts,
		token: token,
		http:  &http.Client{Transport: tr, Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, apiPath string, form url.Values) (json.RawMessage, error) {
	if len(c.hosts) == 0 {
		return nil, fmt.Errorf("no Proxmox hosts configured (set PVE_HOSTS)")
	}
	if c.token == "" {
		return nil, fmt.Errorf("no Proxmox API token configured (set PVE_TOKEN)")
	}
	var lastErr error
	for _, h := range c.hosts {
		var body io.Reader
		var ctype string
		u := h + "/api2/json" + apiPath
		req, err := http.NewRequest(method, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if form != nil {
			body = strings.NewReader(form.Encode())
			req.Body = io.NopCloser(body)
			ctype = "application/x-www-form-urlencoded"
			req.ContentLength = int64(len(form.Encode()))
		}
		req.Header.Set("Authorization", "PVEAPIToken="+c.token)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", h, err)
			continue
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", h, err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: proxmox API %s %s -> HTTP %d: %s", h, method, apiPath, resp.StatusCode, clip(raw))
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("%s: bad API response: %w", h, err)
		}
		return env.Data, nil
	}
	return nil, fmt.Errorf("all Proxmox hosts unreachable, last error: %v", lastErr)
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

type clusterResource struct {
	VMID     int             `json:"vmid"`
	Name     string          `json:"name"`
	Node     string          `json:"node"`
	Type     string          `json:"type"`
	Status   string          `json:"status"`
	Tags     string          `json:"tags"`
	Template json.RawMessage `json:"template"`
}

func splitTags(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
		if t := strings.ToLower(strings.TrimSpace(p)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func isTemplate(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n == 1
	}
	return false
}

func (c *Client) ClusterVMs() ([]Guest, error) {
	data, err := c.do(http.MethodGet, "/cluster/resources?type=vm", nil)
	if err != nil {
		return nil, err
	}
	var res []clusterResource
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("cannot parse cluster resources: %w", err)
	}
	guests := make([]Guest, 0, len(res))
	for _, r := range res {
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}
		guests = append(guests, Guest{
			VMID:     r.VMID,
			Name:     r.Name,
			Node:     r.Node,
			Type:     r.Type,
			Status:   r.Status,
			Tags:     splitTags(r.Tags),
			Template: isTemplate(r.Template),
		})
	}
	sort.Slice(guests, func(i, j int) bool { return guests[i].VMID < guests[j].VMID })
	return guests, nil
}

func (c *Client) GuestStatus(node, typ string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/current", node, typ, vmid)
	data, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var s struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return "", fmt.Errorf("cannot parse guest status: %w", err)
	}
	return s.Status, nil
}

func (c *Client) Shutdown(node, typ string, vmid, timeout int) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/shutdown", node, typ, vmid)
	form := url.Values{"timeout": {fmt.Sprint(timeout)}}
	_, err := c.do(http.MethodPost, path, form)
	return err
}

func (c *Client) Stop(node, typ string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/stop", node, typ, vmid)
	form := url.Values{"overrule-shutdown": {"1"}}
	_, err := c.do(http.MethodPost, path, form)
	return err
}
