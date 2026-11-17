package egress

import (
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
	"sync/atomic"
)

// TargetIdentity is a visible, non-secret signature attached to outbound
// assessment HTTP requests. It is NOT an authentication mechanism.
type TargetIdentity struct {
	UserAgent string            `json:"user_agent"`
	Headers   map[string]string `json:"headers"`
}

var targetIdentity atomic.Pointer[TargetIdentity]

// CurrentTargetIdentity returns a defensive copy, safe for callers to edit.
func CurrentTargetIdentity() TargetIdentity {
	p := targetIdentity.Load()
	if p == nil {
		return TargetIdentity{Headers: map[string]string{}}
	}
	return copyIdentity(*p)
}

func copyIdentity(i TargetIdentity) TargetIdentity {
	c := TargetIdentity{UserAgent: i.UserAgent, Headers: make(map[string]string, len(i.Headers))}
	for k, v := range i.Headers {
		c.Headers[k] = v
	}
	return c
}

var forbiddenTargetHeaders = map[string]bool{
	"Authorization": true, "Cookie": true, "Cookie2": true, "Set-Cookie": true,
	"Host": true, "Content-Length": true, "Transfer-Encoding": true,
	"Connection": true, "Proxy-Connection": true, "Keep-Alive": true,
	"Upgrade": true, "Trailer": true, "Te": true, "Accept-Encoding": true,
	"Content-Type": true, "User-Agent": true,
}

// ValidateTargetIdentity normalizes and bounds the user-visible signature.
// Request framing, authentication, and transport headers are not signatures.
func ValidateTargetIdentity(i TargetIdentity) (TargetIdentity, error) {
	out := TargetIdentity{UserAgent: strings.TrimSpace(i.UserAgent), Headers: map[string]string{}}
	if len(out.UserAgent) > 256 || hasInvalidHeaderValue(out.UserAgent) {
		return out, fmt.Errorf("target User-Agent must be at most 256 bytes and contain no control characters")
	}
	if len(i.Headers) > 16 {
		return out, fmt.Errorf("target headers: at most 16 fields are allowed")
	}
	for rawName, rawValue := range i.Headers {
		name := strings.TrimSpace(rawName)
		if len(name) == 0 || len(name) > 128 || !headerToken(name) {
			return out, fmt.Errorf("invalid target header name %q", rawName)
		}
		key := textproto.CanonicalMIMEHeaderKey(name)
		if forbiddenTargetHeaders[key] || strings.HasPrefix(strings.ToLower(key), "proxy-") || strings.HasPrefix(strings.ToLower(key), "sec-websocket-") {
			return out, fmt.Errorf("target header %q is reserved; use User-Agent's separate field or a non-sensitive identity header", name)
		}
		v := strings.TrimSpace(rawValue)
		if len(v) == 0 || len(v) > 512 || hasInvalidHeaderValue(v) {
			return out, fmt.Errorf("target header %q must have a nonempty value of at most 512 bytes with no control characters", name)
		}
		if _, dup := out.Headers[key]; dup {
			return out, fmt.Errorf("duplicate target header %q (names are case-insensitive)", name)
		}
		out.Headers[key] = v
	}
	return out, nil
}

func headerToken(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func hasInvalidHeaderValue(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// SetTargetIdentity atomically applies a validated identity to new requests.
func SetTargetIdentity(i TargetIdentity) error {
	valid, err := ValidateTargetIdentity(i)
	if err != nil {
		return err
	}
	targetIdentity.Store(&valid)
	return nil
}

// ApplyTargetIdentity modifies a *target* request's header collection. It must
// never be used on LLM calls, update checks or other control-plane requests.
func ApplyTargetIdentity(h http.Header) {
	if h == nil {
		return
	}
	i := targetIdentity.Load()
	if i == nil {
		return
	}
	if i.UserAgent != "" {
		h.Set("User-Agent", i.UserAgent)
	}
	for k, v := range i.Headers {
		h.Set(k, v)
	}
}

func withTargetIdentity(req *http.Request) *http.Request {
	i := targetIdentity.Load()
	if i == nil || (i.UserAgent == "" && len(i.Headers) == 0) {
		return req
	}
	clone := req.Clone(req.Context())
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	// Use the same atomically-loaded snapshot for the entire request.
	if i.UserAgent != "" {
		clone.Header.Set("User-Agent", i.UserAgent)
	}
	for k, v := range i.Headers {
		clone.Header.Set(k, v)
	}
	return clone
}
