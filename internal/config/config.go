package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	PVEHosts      []string
	PVEVerifySSL  bool
	PVEToken      string
	TargetTags    []string
	ExcludeVMIDs  map[int]bool
	DryRun        bool
	InterVMDelayS int
	ForceAfterS   int
	PollIntervalS int
	WebPassword   string
	SessionTTLHrs int
	Port          string
	FrontendMode  string
	ViteDevURL    string
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func getBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if s := strings.ToLower(strings.TrimSpace(p)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func Load() (*Config, error) {
	hosts := splitList(getEnv("PVE_HOSTS", ""))
	tags := splitList(os.Getenv("TARGET_TAGS"))
	if tags == nil {
		tags = []string{}
	}
	if len(hosts) == 0 {
		if h := strings.TrimSpace(os.Getenv("PVE_HOST")); h != "" {
			hosts = []string{h}
		}
	}
	for i, h := range hosts {
		hosts[i] = strings.TrimRight(h, "/")
	}

	exclude := map[int]bool{}
	for _, p := range strings.Split(os.Getenv("EXCLUDE_VMIDS"), ",") {
		if s := strings.TrimSpace(p); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				exclude[n] = true
			}
		}
	}

	c := &Config{
		PVEHosts:      hosts,
		PVEVerifySSL:  getBool("PVE_VERIFY_SSL", false),
		PVEToken:      strings.TrimSpace(os.Getenv("PVE_TOKEN")),
		TargetTags:     tags,
		ExcludeVMIDs:  exclude,
		DryRun:        getBool("DRY_RUN", false),
		InterVMDelayS: getInt("INTER_VM_DELAY_SEC", 2),
		ForceAfterS:   getInt("FORCE_AFTER_SEC", 300),
		PollIntervalS: getInt("POLL_INTERVAL_SEC", 5),
		WebPassword:   os.Getenv("WEB_PASSWORD"),
		SessionTTLHrs: getInt("SESSION_TTL_HOURS", 8),
		Port:          getEnv("PORT", "8080"),
		FrontendMode:  strings.ToLower(getEnv("FRONTEND_MODE", "embed")),
		ViteDevURL:    strings.TrimRight(getEnv("VITE_DEV_URL", "http://localhost:5173"), "/"),
	}
	if c.PollIntervalS < 1 {
		c.PollIntervalS = 1
	}
	if c.SessionTTLHrs < 1 {
		c.SessionTTLHrs = 1
	}
	if c.WebPassword == "" {
		return nil, fmt.Errorf("WEB_PASSWORD is required (refusing to start without a login password)")
	}
	if c.FrontendMode != "embed" && c.FrontendMode != "proxy" {
		return nil, fmt.Errorf("FRONTEND_MODE must be embed or proxy, got %q", c.FrontendMode)
	}
	return c, nil
}
