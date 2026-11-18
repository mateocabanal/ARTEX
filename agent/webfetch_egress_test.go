package agent

import (
	"encoding/json"
	"errors"
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

// When the Managed Proxy Pool is enabled, an already-built WebFetch tool
// must fail closed on proxy failure; normal ARTEX retry remains unchanged when
// the managed policy is off. This test is database-free and loopback-only.
func TestWebFetchNoDirectFallbackWithoutDatabase(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("should-not-be-reached"))
	}))
	defer target.Close()
	const proxy = "http://127.0.0.1:1"
	tools := withWebFetch(t.Context(), nil, proxy, "")
	call := func() (actool.Result, error) {
		return tools[0].Call(t.Context(), json.RawMessage(fetchInputJSON(target.URL)), &actool.ToolContext{})
	}

	// Our fork must retain upstream's fallback when managed routing is off.
	before, err := call()
	if err != nil || before.IsError || hits.Load() != 1 {
		t.Fatalf("upstream direct retry changed: err=%v isError=%v hits=%d", err, before.IsError, hits.Load())
	}
	cleanup := InstallManagedProxyDialGuard(func(configured, addr string) error {
		if configured != proxy || addr != "127.0.0.1:1" {
			return errors.New("managed pool prohibits alternate WebFetch egress")
		}
		return nil
	})
	t.Cleanup(cleanup)

	// Enabling the managed pool must constrain even this existing WebFetch tool.
	blocked, err := call()
	if err != nil || !blocked.IsError || hits.Load() != 1 || strings.Contains(blocked.Flatten(), "should-not-be-reached") {
		t.Fatalf("managed guard bypass: err=%v isError=%v hits=%d", err, blocked.IsError, hits.Load())
	}

	// A direct/stale agent created while capture was off must not connect.
	stale := withWebFetch(t.Context(), nil, "", "")
	staleResult, err := stale[0].Call(t.Context(), json.RawMessage(fetchInputJSON(target.URL)), &actool.ToolContext{})
	if err != nil || !staleResult.IsError || hits.Load() != 1 {
		t.Fatalf("stale direct route bypassed pool: err=%v isError=%v hits=%d", err, staleResult.IsError, hits.Load())
	}

	// Once the policy is disabled, the original retry semantics return.
	cleanup()
	after, err := call()
	if err != nil || after.IsError || hits.Load() != 2 {
		t.Fatalf("normal ARTEX behavior not restored: err=%v isError=%v hits=%d", err, after.IsError, hits.Load())
	}
}
