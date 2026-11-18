package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/managedproxy"
)

// managedProxySelector applies a deterministic policy at the recording proxy,
// where the pool is the only allowed exit whenever enabled.
func (m *Manager) managedProxySelector() func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		if m.proxyPool != nil && m.proxyPool.Enabled() {
			var host string
			if r.URL != nil {
				host = r.URL.Hostname()
			}
			if host == "" {
				host = r.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}
			}
			return m.proxyPool.Select(host)
		}
		// Preserve upstream's exact stored proxy behavior (including
		// validation/rejection performed by SetUpstreamProxy) while the
		// managed pool is disabled. Do not reparse a separate setting here.
		if m.traffic != nil {
			return m.traffic.ConfiguredUpstreamProxy(), nil
		}
		return nil, nil
	}
}

// managedWebFetchDialCheck is invoked before each WebFetch TCP connection.
// When the pool is disabled it is a strict no-op: normal ARTEX's direct
// retry and proxy behavior remain intact. When enabled, only the recording
// proxy is a legal WebFetch dial destination, even for an already-running
// agent created before the pool was enabled.
func (m *Manager) managedWebFetchDialCheck(configuredProxyURL, dialAddress string) error {
	if m.proxyPool == nil || !m.proxyPool.Enabled() {
		return nil
	}
	if m.traffic == nil {
		return errors.New("managed proxy pool requires a recording proxy")
	}
	expected, err := url.Parse(m.traffic.ProxyAddr())
	if err != nil {
		return errors.New("managed proxy pool recording proxy address invalid")
	}
	configured, err := url.Parse(configuredProxyURL)
	if err != nil || configured.Scheme != expected.Scheme ||
		!strings.EqualFold(configured.Host, expected.Host) ||
		configured.Path != "" || configured.RawQuery != "" {
		return errors.New("managed proxy pool blocks WebFetch from using a stale or alternate proxy route")
	}
	if !strings.EqualFold(dialAddress, expected.Host) {
		return errors.New("managed proxy pool blocks WebFetch direct fallback")
	}
	return nil
}

// SetManagedProxyPool changes only a pool of manually-entered authorized proxies.
// Proxy credentials are encrypted by the DB secret setting. A saved placeholder
// keeps an existing endpoint's credential-bearing URL without echoing it to UI.
func (m *Manager) SetManagedProxyPool(input managedproxy.Config) error {
	if m.proxyPool == nil {
		return errors.New("managed proxy pool not initialized")
	}
	old := m.proxyPool.Config()
	for i := range input.Proxies {
		e := &input.Proxies[i]
		if strings.TrimSpace(e.URL) != managedproxy.Mask {
			continue
		}
		found := false
		for _, prev := range old.Proxies {
			if prev.ID == e.ID {
				e.URL = prev.URL
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("proxy %s: no saved address to retain", e.ID)
		}
	}
	valid, err := managedproxy.Validate(input)
	if err != nil {
		return err
	}
	if valid.Enabled && (m.traffic == nil || !m.TrafficEnabled()) {
		return errors.New("enable traffic capture before enabling the managed proxy pool")
	}
	data, err := json.Marshal(valid)
	if err != nil {
		return err
	}
	if err = m.pg.SetSetting(settingManagedProxyPool, string(data)); err != nil {
		return err
	}
	if err = m.proxyPool.Configure(valid); err != nil {
		return err
	}
	// Nonblocking initial probe; the ongoing loop refreshes every five minutes.
	if valid.Enabled {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(managedproxy.MaxProxies)*2*time.Second)
			defer cancel()
			m.proxyPool.Check(ctx)
		}()
	}
	return nil
}
