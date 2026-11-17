package traffic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/artex/egress"
)

// The recording proxy injects the same signature into both plain and intercepted
// target requests even if the noise governor is disabled.
func TestProxyTargetIdentity(t *testing.T) {
	old := egress.CurrentTargetIdentity()
	defer func() { _ = egress.SetTargetIdentity(old) }()
	oldNoise := egress.CurrentProfile()
	defer egress.SetProfile(oldNoise)
	egress.SetProfile(egress.Profile{Name: "off"})
	if err := egress.SetTargetIdentity(egress.TargetIdentity{
		UserAgent: "ARTEX-Authorized/1.0",
		Headers:   map[string]string{"X-Assessment-Id": "integration-lab"},
	}); err != nil {
		t.Fatal(err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "ARTEX-Authorized/1.0" ||
			r.Header.Get("X-Assessment-Id") != "integration-lab" {
			t.Errorf("target headers = %v", r.Header)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	tr, _, proxyURL := startProxy(t)
	client := proxyClient(t, tr, proxyURL)
	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "original")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("response status = %d, want 204", resp.StatusCode)
	}
}
