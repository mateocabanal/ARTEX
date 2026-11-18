package agent

import (
	"context"
	"net"
	"sync/atomic"
)

// managedProxyDialRule is an opt-in, runtime-only outbound restriction for the
// fork's Managed Proxy Pool. With no installed rule, WebFetch has exactly its
// upstream ARTEX direct-retry behavior.
type managedProxyDialRule struct {
	check func(configuredProxyURL, dialAddress string) error
}

var currentManagedProxyDialRule atomic.Pointer[managedProxyDialRule]

// InstallManagedProxyDialGuard installs a deployment-wide rule that runs before
// every WebFetch TCP dial, including its built-in direct retry client. The rule
// must return nil when the managed pool is disabled. The returned cleanup only
// uninstalls this exact installation, not a newer Manager's rule.
func InstallManagedProxyDialGuard(check func(configuredProxyURL, dialAddress string) error) func() {
	if check == nil {
		return func() {}
	}
	rule := &managedProxyDialRule{check: check}
	currentManagedProxyDialRule.Store(rule)
	return func() { currentManagedProxyDialRule.CompareAndSwap(rule, nil) }
}

func managedProxyDialer(proxy string, base func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if rule := currentManagedProxyDialRule.Load(); rule != nil {
			if err := rule.check(proxy, address); err != nil {
				return nil, err
			}
		}
		return base(ctx, network, address)
	}
}
