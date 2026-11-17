package egress

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func preserveTargetIdentity(t *testing.T) {
	t.Helper()
	old := CurrentTargetIdentity()
	t.Cleanup(func() {
		if err := SetTargetIdentity(old); err != nil {
			t.Error(err)
		}
	})
}

func TestTargetIdentityValidation(t *testing.T) {
	tests := []struct {
		name string
		id   TargetIdentity
	}{
		{"newline UA", TargetIdentity{UserAgent: "agent\r\nInjected: yes"}},
		{"too long UA", TargetIdentity{UserAgent: strings.Repeat("a", 257)}},
		{"empty header name", TargetIdentity{Headers: map[string]string{"": "yes"}}},
		{"invalid name", TargetIdentity{Headers: map[string]string{"X-Invalid Header": "yes"}}},
		{"newline value", TargetIdentity{Headers: map[string]string{"X-Test": "yes\r\nInjected: yes"}}},
		{"empty header value", TargetIdentity{Headers: map[string]string{"X-Test": "  "}}},
		{"too long value", TargetIdentity{Headers: map[string]string{"X-Test": strings.Repeat("a", 513)}}},
		{"duplicate case", TargetIdentity{Headers: map[string]string{"x-id": "a", "X-Id": "b"}}},
		{"authorization blocked", TargetIdentity{Headers: map[string]string{"Authorization": "Bearer abc"}}},
		{"proxy blocked", TargetIdentity{Headers: map[string]string{"Proxy-Authorization": "abc"}}},
		{"connection blocked", TargetIdentity{Headers: map[string]string{"Connection": "upgrade"}}},
		{"host blocked", TargetIdentity{Headers: map[string]string{"Host": "elsewhere"}}},
		{"UA as custom header blocked", TargetIdentity{Headers: map[string]string{"user-agent": "something"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateTargetIdentity(tc.id); err == nil {
				t.Fatalf("accepted invalid identity: %+v", tc.id)
			}
		})
	}
	headers := map[string]string{}
	for i := 0; i < 17; i++ {
		headers["X-Test-"+string(rune('A'+i))] = "ok"
	}
	if _, err := ValidateTargetIdentity(TargetIdentity{Headers: headers}); err == nil {
		t.Fatal("accepted 17 headers")
	}

	got, err := ValidateTargetIdentity(TargetIdentity{UserAgent: " test/1 ", Headers: map[string]string{"x-assessment-id": "  allowed  "}})
	if err != nil || got.UserAgent != "test/1" || got.Headers["X-Assessment-Id"] != "allowed" {
		t.Fatalf("normalized = %+v, %v", got, err)
	}
}

func TestTargetIdentityOnOutgoingHTTP(t *testing.T) {
	preserveTargetIdentity(t)
	keepProfile(t)
	if err := SetTargetIdentity(TargetIdentity{}); err != nil {
		t.Fatal(err)
	}
	SetProfile(Profile{Name: "off"})
	recorder := &recordRT{}
	if tr := Current().Transport("identity", recorder); tr != recorder {
		t.Fatal("identity disabled: Transport is not a strict passthrough")
	}

	identity := TargetIdentity{UserAgent: "ARTEX-Approved/1.0", Headers: map[string]string{"X-Assessment-Id": "abc-123"}}
	if err := SetTargetIdentity(identity); err != nil {
		t.Fatal(err)
	}
	identity.Headers["X-Assessment-Id"] = "tampered" // must not modify the published config
	current := CurrentTargetIdentity()
	current.Headers["X-Assessment-Id"] = "tampered" // caller must get a copy

	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	req.Header.Set("User-Agent", "original-UA")
	req.Header.Set("X-Assessment-Id", "original-id")
	req.Header.Set("Authorization", "test-secret")
	tr := Dynamic("identity", recorder)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if recorder.got == req || recorder.got.Header.Get("User-Agent") != "ARTEX-Approved/1.0" ||
		recorder.got.Header.Get("X-Assessment-Id") != "abc-123" ||
		recorder.got.Header.Get("Authorization") != "test-secret" {
		t.Fatalf("outgoing headers = %v", recorder.got.Header)
	}
	if req.Header.Get("User-Agent") != "original-UA" || req.Header.Get("X-Assessment-Id") != "original-id" {
		t.Fatalf("caller request mutated: %v", req.Header)
	}

	// Changing the identity takes effect for the next request, even on the same client.
	if err := SetTargetIdentity(TargetIdentity{UserAgent: "ARTEX-Updated/2.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if recorder.got.Header.Get("User-Agent") != "ARTEX-Updated/2.0" ||
		recorder.got.Header.Get("X-Assessment-Id") != "original-id" {
		t.Fatalf("changed identity not applied: %v", recorder.got.Header)
	}
}

func TestTargetIdentityOnGovernedHTTP(t *testing.T) {
	preserveTargetIdentity(t)
	if err := SetTargetIdentity(TargetIdentity{UserAgent: "ARTEX-Governed/1", Headers: map[string]string{"X-Bug-Bounty": "lab"}}); err != nil {
		t.Fatal(err)
	}
	g := New(open)
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	rec := &recordRT{}
	if _, err := g.Transport("identity", rec).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if rec.got.Header.Get("User-Agent") != "ARTEX-Governed/1" || rec.got.Header.Get("X-Bug-Bounty") != "lab" {
		t.Fatalf("governed request identity missing: %v", rec.got.Header)
	}
}
