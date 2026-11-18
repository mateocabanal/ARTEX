package traffic

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/artex/managedproxy"
)

func TestManagedPoolThroughRecordingProxy(t *testing.T) {
	var targetHits, upstreamHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = w.Write([]byte("authorized-local-fixture"))
	}))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		tu, _ := url.Parse(target.URL)
		// The go-mitmproxy upstream dials CONNECT tunnels, even for HTTP.
		if r.Method != http.MethodConnect || r.Host != tu.Host {
			http.Error(w, "outside authorized local fixture", http.StatusForbidden)
			return
		}
		remote, err := net.Dial("tcp", tu.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			remote.Close()
			http.Error(w, "cannot tunnel", 500)
			return
		}
		client, rw, err := hj.Hijack()
		if err != nil {
			remote.Close()
			return
		}
		if _, err = fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			client.Close()
			remote.Close()
			return
		}
		if err = rw.Flush(); err != nil {
			client.Close()
			remote.Close()
			return
		}
		go func() { defer remote.Close(); defer client.Close(); _, _ = io.Copy(remote, rw) }()
		_, _ = io.Copy(client, remote)
	}))
	defer upstream.Close()

	pool := managedproxy.New()
	if err := pool.Configure(managedproxy.Config{Enabled: true,
		AllowedHosts: []string{"127.0.0.1"},
		Proxies:      []managedproxy.Endpoint{{ID: "owned", Name: "Local fixture proxy", URL: upstream.URL, Authorized: true}},
	}); err != nil {
		t.Fatal(err)
	}
	pool.Check(context.Background())
	if !pool.Status().Proxies[0].Healthy {
		t.Fatal("local upstream wasn't reachable")
	}

	tr, _, proxyURL := startProxy(t)
	tr.SetUpstreamSelector(func(r *http.Request) (*url.URL, error) {
		return pool.Select(r.URL.Hostname())
	})
	tr.SetTargetPolicy(pool.AllowHost)
	client := proxyClient(t, tr, proxyURL)
	res, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(data) != "authorized-local-fixture" || upstreamHits.Load() != 1 || targetHits.Load() != 1 {
		t.Fatalf("unexpected hop chain: status=%d data=%q upstream=%d target=%d", res.StatusCode, data, upstreamHits.Load(), targetHits.Load())
	}
	if pool.Status().AdmittedRequests != 1 {
		t.Fatalf("request admission not counted: %+v", pool.Status())
	}

	// A destination not present in the managed allowlist must not pass directly,
	// even though the recording proxy could ordinarily dial it.
	pool2 := managedproxy.New()
	if err := pool2.Configure(managedproxy.Config{Enabled: true, AllowedHosts: []string{"other.example.test"},
		Proxies: []managedproxy.Endpoint{{ID: "owned", Name: "Local fixture proxy", URL: upstream.URL, Authorized: true}},
	}); err != nil {
		t.Fatal(err)
	}
	pool2.Check(context.Background())
	tr.SetUpstreamSelector(func(r *http.Request) (*url.URL, error) { return pool2.Select(r.URL.Hostname()) })
	tr.SetTargetPolicy(pool2.AllowHost)
	_, err = client.Get(target.URL)
	if err == nil && targetHits.Load() != 1 {
		t.Fatal("out-of-scope request reached target")
	}
	if upstreamHits.Load() != 1 || targetHits.Load() != 1 {
		t.Fatalf("bypassed policy: upstream=%d target=%d", upstreamHits.Load(), targetHits.Load())
	}
	if pool2.Status().BlockedRequests == 0 {
		t.Fatal("blocked request was not counted")
	}
}
