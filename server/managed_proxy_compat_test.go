package server

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Autumn-27/artex/egress"
	"github.com/Autumn-27/artex/managedproxy"
	"github.com/Autumn-27/artex/traffic"
)

// No PostgreSQL needed: default ARTEX networking stays unchanged unless an
// operator explicitly turns on a valid managed pool.
func TestManagedWebFetchDialGuardIsOptIn(t *testing.T) {
	tr, err := traffic.Open(t.TempDir(), "127.0.0.1:18788")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	pool := managedproxy.New()
	m := &Manager{proxyPool: pool, traffic: tr}
	capture := tr.ProxyAddr()
	pu, err := url.Parse(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.managedWebFetchDialCheck("", "outside.example.test:443"); err != nil {
		t.Fatalf("disabled pool changed upstream behavior: %v", err)
	}
	cfg := managedproxy.Config{
		Enabled: true, AllowedHosts: []string{"lab.example.test"},
		Proxies: []managedproxy.Endpoint{
			{ID: "owned", Name: "Owned lab proxy", URL: "socks5://127.0.0.1:1080", Authorized: true},
		},
	}
	if err := pool.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		proxy string
		dial  string
	}{
		{"", "outside.example.test:443"},
		{"http://127.0.0.1:18888", "127.0.0.1:18888"},
		{capture, "outside.example.test:443"},
		{capture, "lab.example.test:80"},
	} {
		if err := m.managedWebFetchDialCheck(tc.proxy, tc.dial); err == nil {
			t.Errorf("managed pool accepted alternate route proxy=%q dial=%q", tc.proxy, tc.dial)
		}
	}
	// A valid in-process recording proxy path is allowed, including an egress
	// pace marker encoded in the proxy URL's userinfo.
	for _, proxy := range []string{capture, egress.MarkProxyURL(capture)} {
		if err := m.managedWebFetchDialCheck(proxy, pu.Host); err != nil {
			t.Errorf("recording proxy rejected %q: %v", proxy, err)
		}
	}
	if err := pool.Configure(managedproxy.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := m.managedWebFetchDialCheck("", "outside.example.test:443"); err != nil {
		t.Fatalf("pool off must restore upstream behavior: %v", err)
	}
}

// Existing deployments using the single global_proxy setting must still exit
// through that same URL whenever the managed pool is disabled.
func TestManagedProxyOffPreservesLegacyGlobalProxy(t *testing.T) {
	tr, err := traffic.Open(t.TempDir(), "127.0.0.1:18789")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	const original = "socks5://user:pass@127.0.0.1:1080"
	if err := tr.SetUpstreamProxy(original); err != nil {
		t.Fatal(err)
	}
	m := &Manager{traffic: tr, proxyPool: managedproxy.New()}
	req := httptest.NewRequest("GET", "http://lab.example.test/", nil)
	u, err := m.managedProxySelector()(req)
	if err != nil || u == nil || u.String() != original {
		t.Fatalf("legacy global proxy changed: %v %v", u, err)
	}
	if err := tr.SetUpstreamProxy("socks4://invalid.example.test:1080"); err == nil {
		t.Fatal("invalid global proxy unexpectedly accepted")
	}
	u, err = m.managedProxySelector()(req)
	if err != nil || u == nil || u.String() != original {
		t.Fatalf("invalid attempted update changed legacy proxy: %v %v", u, err)
	}
	if err := tr.SetUpstreamProxy(""); err != nil {
		t.Fatal(err)
	}
	u, err = m.managedProxySelector()(req)
	if err != nil || u != nil {
		t.Fatalf("cleared global proxy should restore direct dialing: %v %v", u, err)
	}
}
