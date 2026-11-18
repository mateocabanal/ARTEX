package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/managedproxy"
)

func TestManagedProxyPoolSettingsAPI(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer func() {
		_, _ = m.pg.Exec(`DELETE FROM settings WHERE key IN ($1,$2)`, settingManagedProxyPool, settingTrafficCapture)
	}()
	// Reset persisted state in the test DB.
	if _, err := m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingManagedProxyPool); err != nil {
		t.Fatal(err)
	}
	if err := m.proxyPool.Configure(managedproxy.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetTrafficEnabled(false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	defer stopTestServer(t, s, cancel)
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	const key = "extra-long-password-that-should-not-leak"
	payload := `{"enabled":true,"allowed_hosts":["lab.example.test"],"proxies":[{"id":"owned","name":"Owned proxy","url":"http://user:extra-long-password-that-should-not-leak@127.0.0.1:9","authorized":true}]}`
	if code, _ := request("PUT", "/api/settings/proxy-pool", payload); code != 400 {
		t.Fatalf("enabled without capture: status %d", code)
	}
	if err := m.SetTrafficEnabled(true); err != nil {
		t.Fatal(err)
	}
	if code, body := request("PUT", "/api/settings/proxy-pool", payload); code != 200 || strings.Contains(body, key) {
		t.Fatalf("PUT status %d, leaked=%v", code, strings.Contains(body, key))
	}
	if err := m.SetTrafficEnabled(false); err == nil {
		t.Fatal("allowed recording proxy to be disabled while pool enabled")
	}
	code, body := request("GET", "/api/settings/proxy-pool", "")
	if code != 200 || strings.Contains(body, key) {
		t.Fatalf("GET status %d, leaked=%v", code, strings.Contains(body, key))
	}
	var status managedproxy.Status
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Proxies) != 1 || !status.Proxies[0].HasCredentials {
		t.Fatalf("missing masked credential indicator: %+v", status)
	}
	if _, err := m.managedProxySelector()(httptest.NewRequest("GET", "http://not-allowed.example.test/", nil)); err == nil {
		t.Fatal("out-of-scope target allowed")
	}
	if _, err := m.managedProxySelector()(httptest.NewRequest("GET", "http://lab.example.test/", nil)); err == nil {
		t.Fatal("connected directly before a healthy probe")
	}
	raw, ok, err := m.pg.GetSetting(settingManagedProxyPool)
	if err != nil || !ok || !strings.Contains(raw, key) {
		t.Fatalf("decrypted stored config %q, err=%v", raw, err)
	}
	var sealed string
	if err := m.pg.QueryRow(`SELECT value FROM settings WHERE key=$1`, settingManagedProxyPool).Scan(&sealed); err != nil || !strings.HasPrefix(sealed, "enc:v1:") {
		t.Fatalf("proxy URL not encrypted at rest: %v", err)
	}
	masked := `{"enabled":true,"allowed_hosts":["lab.example.test"],"proxies":[{"id":"owned","name":"Owned proxy","url":"__masked__","authorized":true}]}`
	if code, body := request("PUT", "/api/settings/proxy-pool", masked); code != 200 || strings.Contains(body, key) {
		t.Fatalf("masked retention status %d", code)
	}
	if cfg := m.proxyPool.Config(); !strings.Contains(cfg.Proxies[0].URL, key) {
		t.Fatal("masked update lost stored credentials")
	}
	if code, _ := request("PUT", "/api/settings/proxy-pool", `{"enabled":false,"proxies":[]}`); code != 200 {
		t.Fatalf("disable status %d", code)
	}
	if err := m.SetTrafficEnabled(false); err != nil {
		t.Fatal(err)
	}
	if code, _ := request("POST", "/api/settings/proxy-pool/check", "{}"); code != 200 {
		t.Fatalf("manual check status %d", code)
	}
	_ = http.MethodGet
}
