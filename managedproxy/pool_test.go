package managedproxy

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// listener is a local authorized proxy endpoint fixture. No internet traffic.
func listener(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return "http://" + ln.Addr().String(), func() { _ = ln.Close() }
}
func config(urls ...string) Config {
	cfg := Config{Enabled: true, AllowedHosts: []string{"lab.example.test", "192.0.2.9"}}
	for i, u := range urls {
		cfg.Proxies = append(cfg.Proxies, Endpoint{ID: "p" + string(rune('1'+i)), Name: "Managed " + string(rune('1'+i)), URL: u, Authorized: true})
	}
	return cfg
}
func TestValidateExplicitScopeAndOwnership(t *testing.T) {
	raw := config("http://127.0.0.1:8080")
	if _, err := Validate(raw); err != nil {
		t.Fatal(err)
	}
	ipv6 := config("http://127.0.0.1:8080")
	ipv6.AllowedHosts = []string{"2001:db8::1"}
	if _, err := Validate(ipv6); err != nil {
		t.Fatalf("valid exact IPv6 target rejected: %v", err)
	}
	samples := []struct {
		name   string
		change func(*Config)
	}{
		{"wildcard", func(c *Config) { c.AllowedHosts = []string{"*.example.test"} }},
		{"unapproved", func(c *Config) { c.Proxies[0].Authorized = false }},
		{"unscoped", func(c *Config) { c.AllowedHosts = nil }},
		{"no proxy", func(c *Config) { c.Proxies = nil }},
		{"invalid URL", func(c *Config) { c.Proxies[0].URL = "socks4://127.0.0.1:1080" }},
		{"embedded query", func(c *Config) { c.Proxies[0].URL = "http://127.0.0.1:8080/?x=1" }},
		{"duplicate ID", func(c *Config) { c.Proxies = append(c.Proxies, c.Proxies[0]) }},
	}
	for _, tc := range samples {
		t.Run(tc.name, func(t *testing.T) {
			c := config("http://127.0.0.1:8080")
			tc.change(&c)
			if _, err := Validate(c); err == nil {
				t.Fatalf("accepted %+v", c)
			}
		})
	}
}
func TestStickyFailoverAndFailClosed(t *testing.T) {
	first, closeFirst := listener(t)
	second, closeSecond := listener(t)
	defer closeSecond()
	p := New()
	if err := p.Configure(config(first, second)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Select("lab.example.test"); err == nil {
		t.Fatal("unprobed pool must fail closed")
	}
	p.Check(context.Background())
	a, err := p.Select("lab.example.test")
	if err != nil || a.String() != first {
		t.Fatalf("initial selection %v %v", a, err)
	}
	for i := 0; i < 5; i++ {
		next, err := p.Select("lab.example.test")
		if err != nil || next.String() != first {
			t.Fatalf("not sticky %v %v", next, err)
		}
	}
	if _, err := p.Select("other.example.test"); err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("out-of-scope %v", err)
	}
	closeFirst()
	p.Check(context.Background())
	b, err := p.Select("lab.example.test")
	if err != nil || b.String() != second {
		t.Fatalf("failover %v %v", b, err)
	}
	status := p.Status()
	if status.Proxies[1].Selections != 1 || status.Proxies[1].Failovers != 1 || status.Proxies[0].Healthy {
		t.Fatalf("unexpected metrics %+v", status)
	}
	closeSecond()
	p.Check(context.Background())
	if _, err := p.Select("lab.example.test"); err == nil {
		t.Fatal("no healthy proxies must fail closed")
	}
}
func TestDisabledPoolDoesNotInterceptOrdinaryTraffic(t *testing.T) {
	p := New()
	if err := p.AllowHost("host-with-unexpected%value"); err != nil {
		t.Fatalf("disabled pool should not alter pre-existing traffic: %v", err)
	}
	if err := p.Configure(Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := p.AllowHost(""); err != nil {
		t.Fatalf("disabled check: %v", err)
	}
}

func TestCredentialsMaskedAndUpdate(t *testing.T) {
	p := New()
	cfg := config("socks5://operator:supersecret@127.0.0.1:1080")
	if err := p.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	s := p.Status()
	if !s.Proxies[0].HasCredentials || strings.Contains(s.Proxies[0].URL, "operator") || strings.Contains(s.Proxies[0].URL, "supersecret") {
		t.Fatalf("credentials exposed: %+v", s)
	}
	if p.Config().Proxies[0].URL != cfg.Proxies[0].URL {
		t.Fatal("persistence view must retain credentials")
	}
}
func TestSOCKS5ProbeNoTargetTraffic(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 3)
		_, _ = c.Read(b)
		_, _ = c.Write([]byte{0x05, 0x00})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	latency, err := Probe(ctx, "socks5://"+ln.Addr().String())
	if err != nil || latency < 0 {
		t.Fatalf("SOCKS greeting: %s %v", latency, err)
	}
}
