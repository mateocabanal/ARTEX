package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/artex/egress"
	"github.com/Autumn-27/artex/intercept"
	actool "github.com/Autumn-27/norma/tool"
)

// pinNoiseOff runs t with the off noise profile, whatever ARTEX_NOISE_PROFILE says.
func pinNoiseOff(t *testing.T) {
	old := egressGovernor
	egressGovernor = func() *egress.Governor { return egress.New(egress.Profile{Name: "off"}) }
	t.Cleanup(func() { egressGovernor = old })
}

// Under a profile, two WebFetch calls on the same URL in one task both run the
// tool and its redirect gate: WebFetch results are never cached.
func TestWebFetchProfileDoesNotCache(t *testing.T) {
	pair := newRedirectPair(t)
	var checks atomic.Int64
	inner := actool.NewWebFetch(actool.WebFetchConfig{CheckRedirect: func(*http.Request, []*http.Request) error {
		checks.Add(1)
		return nil
	}})
	balanced := egress.New(egress.Profiles["balanced"])
	old := egressGovernor
	egressGovernor = func() *egress.Governor { return balanced }
	t.Cleanup(func() { egressGovernor = old })
	tool := governedFetch{inner}
	ctx := intercept.WithTargetScope(t.Context(), intercept.TargetScope{TaskID: "7"})
	for range 2 {
		res, err := tool.Call(ctx, json.RawMessage(fetchInputJSON(pair.start)), &actool.ToolContext{})
		if err != nil || res.IsError {
			t.Fatalf("WebFetch: err=%v result=%q", err, res.Flatten())
		}
	}
	if checks.Load() != 2 || pair.startHits.Load() != 2 || pair.finalHits.Load() != 2 {
		t.Fatalf("redirect checks %d, start hits %d, final hits %d; want 2 each (no cache hit)",
			checks.Load(), pair.startHits.Load(), pair.finalHits.Load())
	}
}

// A configured upstream proxy is a routing commitment, not a hint. WebFetch
// must never bypass a failed proxy by trying the target directly. This test
// needs no PostgreSQL and runs against a local HTTP server only.
func TestWebFetchNoDirectFallbackWithoutDatabase(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("should-not-be-reached"))
	}))
	defer target.Close()
	tools := withWebFetch(t.Context(), nil, "http://127.0.0.1:1", "")
	res, err := tools[0].Call(t.Context(), json.RawMessage(fetchInputJSON(target.URL)), &actool.ToolContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || hits.Load() != 0 || strings.Contains(res.Flatten(), "should-not-be-reached") {
		t.Fatalf("WebFetch bypassed configured proxy: tool error=%v target hits=%d", res.IsError, hits.Load())
	}
}
