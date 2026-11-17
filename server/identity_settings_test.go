package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/egress"
)

func TestTargetIdentitySettingsHTTP(t *testing.T) {
	skipWithoutDSN(t)
	previous := egress.CurrentTargetIdentity()
	defer func() { _ = egress.SetTargetIdentity(previous) }()

	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer func() {
		_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingTargetIdentity)
	}()
	if _, err := m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingTargetIdentity); err != nil {
		t.Fatal(err)
	}
	if err := egress.SetTargetIdentity(egress.TargetIdentity{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	defer stopTestServer(t, s, cancel)

	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/settings", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("response %q: %v", rec.Body.String(), err)
		}
		return rec.Code, out
	}

	if code, out := do(http.MethodGet, ""); code != 200 || out["target_user_agent"] != "" {
		t.Fatalf("empty identity: %d %v", code, out)
	}
	body := `{"target_user_agent":"ARTEX-Authorized/1.0","target_headers":{"x-assessment-id":"test-123","X-Bug-Bounty":"yes"}}`
	code, out := do(http.MethodPut, body)
	if code != 200 || out["target_user_agent"] != "ARTEX-Authorized/1.0" {
		t.Fatalf("PUT identity: %d %v", code, out)
	}
	h, ok := out["target_headers"].(map[string]any)
	if !ok || h["X-Assessment-Id"] != "test-123" || h["X-Bug-Bounty"] != "yes" {
		t.Fatalf("normalized header values: %v", out)
	}
	stored := startupTargetIdentity(m.pg)
	if stored.UserAgent != "ARTEX-Authorized/1.0" || stored.Headers["X-Assessment-Id"] != "test-123" {
		t.Fatalf("persisted identity: %+v", stored)
	}
	for _, bad := range []string{
		`{"target_headers":{"Authorization":"abc"}}`,
		`{"target_headers":{"X Invalid":"bad"}}`,
		`{"target_headers":{"X-Empty":"  "}}`,
		`{"target_headers":{"x-id":"a","X-Id":"b"}}`,
	} {
		if code, out := do(http.MethodPut, bad); code != http.StatusBadRequest {
			t.Errorf("accepted unsafe PUT %s: %d %v", bad, code, out)
		}
	}
	if got := egress.CurrentTargetIdentity(); got.UserAgent != stored.UserAgent || got.Headers["X-Assessment-Id"] != "test-123" {
		t.Fatalf("identity changed after rejected update: %+v", got)
	}
	if code, out := do(http.MethodPut, `{"target_user_agent":"","target_headers":{}}`); code != 200 || out["target_user_agent"] != "" {
		t.Fatalf("clear identity: %d %v", code, out)
	}
	if got := egress.CurrentTargetIdentity(); got.UserAgent != "" || len(got.Headers) != 0 {
		t.Fatalf("clear did not apply: %+v", got)
	}
}
