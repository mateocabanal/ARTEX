// Package managedproxy provides a manually configured, explicitly scoped
// upstream proxy pool. It never downloads public proxy lists or rotates IPs.
package managedproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const CheckInterval = 5 * time.Minute
const MaxProxies = 8
const Mask = "__masked__"

type Endpoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Authorized bool   `json:"authorized"`
}
type Config struct {
	Enabled      bool       `json:"enabled"`
	AllowedHosts []string   `json:"allowed_hosts"`
	Proxies      []Endpoint `json:"proxies"`
}
type Health struct {
	Healthy    bool   `json:"healthy"`
	CheckedAt  string `json:"checked_at,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	Reason     string `json:"reason,omitempty"`
	Selections uint64 `json:"selections"`
	Failovers  uint64 `json:"failovers"`
}
type EndpointStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	HasCredentials bool   `json:"has_credentials"`
	Authorized     bool   `json:"authorized"`
	Health
}
type Status struct {
	Enabled              bool             `json:"enabled"`
	AllowedHosts         []string         `json:"allowed_hosts"`
	Proxies              []EndpointStatus `json:"proxies"`
	CheckIntervalSeconds int              `json:"check_interval_seconds"`
	BoundHosts           int              `json:"bound_hosts"`
	AdmittedRequests     uint64           `json:"admitted_requests"`
	BlockedRequests      uint64           `json:"blocked_requests"`
}
type Pool struct {
	mu        sync.Mutex
	config    Config
	version   uint64
	health    map[string]Health
	hostProxy map[string]string
	admitted  uint64
	blocked   uint64
}

func New() *Pool { return &Pool{health: make(map[string]Health), hostProxy: make(map[string]string)} }

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,251}[a-z0-9]$`)

func normalizeHost(s string) (string, error) {
	s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
	if len(s) > 253 || s == "" {
		return "", errors.New("expected exact hostname or IP")
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, "/:*?#@") {
		return "", errors.New("expected exact hostname or IP without scheme, port, or wildcard")
	}
	if s == "localhost" {
		return s, nil
	}
	if !hostPattern.MatchString(s) || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return "", errors.New("invalid target hostname")
	}
	return s, nil
}
func validateProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("malformed proxy URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
		return nil, errors.New("only http, https and socks5 proxy schemes are supported")
	}
	if u.Hostname() == "" || u.Port() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("proxy must have a host and explicit port, with no path/query/fragment")
	}
	if _, err := net.LookupPort("tcp", u.Port()); err != nil {
		return nil, errors.New("invalid proxy port")
	}
	return u, nil
}

// Validate rejects anonymous list imports by requiring explicit individual
// ownership acknowledgement and per-target scope. It is not proof of ownership.
func Validate(cfg Config) (Config, error) {
	out := Config{Enabled: cfg.Enabled}
	if len(cfg.Proxies) > MaxProxies {
		return out, fmt.Errorf("at most %d proxies", MaxProxies)
	}
	if len(cfg.AllowedHosts) > 64 {
		return out, errors.New("at most 64 allowed target hosts")
	}
	seenHosts := map[string]bool{}
	for _, h := range cfg.AllowedHosts {
		name, err := normalizeHost(h)
		if err != nil {
			return out, fmt.Errorf("allowed_hosts: %w", err)
		}
		if !seenHosts[name] {
			seenHosts[name] = true
			out.AllowedHosts = append(out.AllowedHosts, name)
		}
	}
	seenIDs := map[string]bool{}
	for _, e := range cfg.Proxies {
		if !idPattern.MatchString(e.ID) || seenIDs[e.ID] {
			return out, errors.New("proxy ID must be unique and use 1-32 letters, digits, _ or -")
		}
		seenIDs[e.ID] = true
		if len(strings.TrimSpace(e.Name)) == 0 || len(e.Name) > 80 {
			return out, fmt.Errorf("proxy %s: name must be 1-80 characters", e.ID)
		}
		if !e.Authorized {
			return out, fmt.Errorf("proxy %s: confirm ownership or explicit permission", e.ID)
		}
		if _, err := validateProxyURL(e.URL); err != nil {
			return out, fmt.Errorf("proxy %s: %w", e.ID, err)
		}
		out.Proxies = append(out.Proxies, Endpoint{ID: e.ID, Name: strings.TrimSpace(e.Name), URL: strings.TrimSpace(e.URL), Authorized: true})
	}
	if out.Enabled && (len(out.Proxies) == 0 || len(out.AllowedHosts) == 0) {
		return out, errors.New("enabling the pool requires at least one authorized proxy and one exact allowed target host")
	}
	return out, nil
}

func (p *Pool) Configure(c Config) error {
	valid, err := Validate(c)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.config = valid
	p.version++
	p.health = map[string]Health{}
	p.hostProxy = map[string]string{}
	p.admitted, p.blocked = 0, 0
	p.mu.Unlock()
	return nil
}
func (p *Pool) Config() Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.config
	c.AllowedHosts = append([]string(nil), c.AllowedHosts...)
	c.Proxies = append([]Endpoint(nil), c.Proxies...)
	return c
}
func (p *Pool) Enabled() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.config.Enabled }
func (p *Pool) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Status{Enabled: p.config.Enabled, AllowedHosts: append([]string(nil), p.config.AllowedHosts...), CheckIntervalSeconds: int(CheckInterval.Seconds()), BoundHosts: len(p.hostProxy), AdmittedRequests: p.admitted, BlockedRequests: p.blocked}
	for _, e := range p.config.Proxies {
		u, _ := url.Parse(e.URL)
		safe := *u
		has := u.User != nil
		safe.User = nil
		s.Proxies = append(s.Proxies, EndpointStatus{ID: e.ID, Name: e.Name, URL: safe.String(), HasCredentials: has, Authorized: e.Authorized, Health: p.health[e.ID]})
	}
	if s.Proxies == nil {
		s.Proxies = []EndpointStatus{}
	}
	if s.AllowedHosts == nil {
		s.AllowedHosts = []string{}
	}
	return s
}

// AllowHost is an independent request-level check for the recording proxy.
// It does not select or rotate an upstream, so it can be called on every HTTP
// message even when several messages share one established CONNECT tunnel.
func (p *Pool) AllowHost(host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.config.Enabled {
		return nil
	}
	normalized, err := normalizeHost(host)
	if err != nil {
		p.blocked++
		return err
	}
	allowed := false
	for _, h := range p.config.AllowedHosts {
		if h == normalized {
			allowed = true
			break
		}
	}
	if !allowed {
		p.blocked++
		return fmt.Errorf("target %s not in managed proxy allowlist", normalized)
	}
	for _, e := range p.config.Proxies {
		if p.health[e.ID].Healthy {
			p.admitted++
			return nil
		}
	}
	p.blocked++
	return errors.New("managed proxy pool has no healthy upstream; request blocked")
}

// Select pins a target host to one reachable proxy. Existing bindings stay
// stable while healthy; a down proxy is replaced with another healthy one.
// There is never a direct fallback or retry/replay of target requests.
func (p *Pool) Select(host string) (*url.URL, error) {
	host, err := normalizeHost(host)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.config.Enabled {
		return nil, errors.New("managed proxy pool disabled")
	}
	allowed := false
	for _, h := range p.config.AllowedHosts {
		if h == host {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("target %s not in managed proxy allowlist", host)
	}
	var candidate *Endpoint
	old := p.hostProxy[host]
	for i := range p.config.Proxies {
		e := &p.config.Proxies[i]
		if e.ID == old && p.health[e.ID].Healthy {
			candidate = e
			break
		}
	}
	if candidate == nil {
		for i := range p.config.Proxies {
			e := &p.config.Proxies[i]
			if p.health[e.ID].Healthy {
				candidate = e
				break
			}
		}
	}
	if candidate == nil {
		return nil, errors.New("managed proxy pool has no healthy upstream; direct fallback blocked")
	}
	h := p.health[candidate.ID]
	h.Selections++
	if old != "" && old != candidate.ID {
		h.Failovers++
	}
	p.health[candidate.ID] = h
	p.hostProxy[host] = candidate.ID
	return url.Parse(candidate.URL)
}

// Probe verifies proxy endpoint reachability, not target connectivity. It makes
// no requests to third-party sites and never downloads proxy lists.
func Probe(ctx context.Context, raw string) (time.Duration, error) {
	u, err := validateProxyURL(raw)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	dial := &net.Dialer{Timeout: 1500 * time.Millisecond}
	start := time.Now()
	c, err := dial.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if u.Scheme == "https" {
		tlsConn := tls.Client(c, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return 0, err
		}
		c = tlsConn
	}
	if u.Scheme == "socks5" {
		// Offer no-auth and username/password; require a valid SOCKS5 greeting.
		methods := []byte{0x05, 0x01, 0x00}
		if u.User != nil {
			methods = []byte{0x05, 0x02, 0x00, 0x02}
		}
		if _, err = c.Write(methods); err != nil {
			return 0, err
		}
		reply := make([]byte, 2)
		if _, err = io.ReadFull(c, reply); err != nil {
			return 0, err
		}
		if reply[0] != 0x05 || reply[1] == 0xff {
			return 0, errors.New("invalid SOCKS5 negotiation")
		}
		if reply[1] == 0x02 {
			username := u.User.Username()
			password, _ := u.User.Password()
			if len(username) > 255 || len(password) > 255 {
				return 0, errors.New("SOCKS credentials too long")
			}
			msg := append([]byte{0x01, byte(len(username))}, []byte(username)...)
			msg = append(msg, byte(len(password)))
			msg = append(msg, []byte(password)...)
			if _, err = c.Write(msg); err != nil {
				return 0, err
			}
			if _, err = io.ReadFull(c, reply); err != nil {
				return 0, err
			}
			if reply[1] != 0 {
				return 0, errors.New("SOCKS authentication failed")
			}
		} else if reply[1] != 0x00 {
			return 0, errors.New("unsupported SOCKS5 authentication method")
		}
	}
	return time.Since(start), nil
}
func (p *Pool) Check(ctx context.Context) {
	p.mu.Lock()
	cfg := p.config
	version := p.version
	nodes := append([]Endpoint(nil), cfg.Proxies...)
	p.mu.Unlock()
	for _, e := range nodes {
		latency, err := Probe(ctx, e.URL)
		p.mu.Lock()
		if p.version == version {
			h := p.health[e.ID]
			h.Healthy = err == nil
			h.CheckedAt = time.Now().UTC().Format(time.RFC3339)
			h.LatencyMs = latency.Milliseconds()
			if err != nil {
				h.Reason = "proxy endpoint unreachable or handshake failed"
			} else {
				h.Reason = ""
			}
			p.health[e.ID] = h
		}
		p.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}
func (p *Pool) Run(ctx context.Context) {
	if p.Enabled() {
		p.Check(ctx)
	}
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.Enabled() {
				p.Check(ctx)
			}
		}
	}
}
