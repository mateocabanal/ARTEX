package traffic

import (
	"net/http"
	"net/url"
)

// managedUpstreamSelector takes precedence over the legacy single upstream URL.
// Selector errors are propagated; they are never converted into direct dialing.
type managedUpstreamSelector struct {
	pick func(*http.Request) (*url.URL, error)
}

type managedTargetPolicy struct {
	check func(string) error
}

// SetTargetPolicy applies a target host constraint to every HTTP request,
// including multiple requests reusing an established upstream CONNECT tunnel.
func (t *Traffic) SetTargetPolicy(check func(string) error) {
	if check == nil {
		t.policy.Store(nil)
		return
	}
	t.policy.Store(&managedTargetPolicy{check: check})
}
func (t *Traffic) checkTargetHost(host string) error {
	if p := t.policy.Load(); p != nil {
		return p.check(host)
	}
	return nil
}

// SetUpstreamSelector installs an optional, concurrency-safe upstream policy.
// nil restores the existing configured global proxy behavior.
func (t *Traffic) SetUpstreamSelector(pick func(*http.Request) (*url.URL, error)) {
	if pick == nil {
		t.selector.Store(nil)
		return
	}
	t.selector.Store(&managedUpstreamSelector{pick: pick})
}
func (t *Traffic) selectUpstream(r *http.Request) (*url.URL, error) {
	if s := t.selector.Load(); s != nil {
		return s.pick(r)
	}
	return t.upstream.Load(), nil
}
