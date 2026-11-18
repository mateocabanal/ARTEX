# Managed Proxy Pool (ARTEX fork)

This feature manages **only proxies you own or are expressly authorized to use**.
It does not import proxy lists, rotate public exit IPs, or attempt to bypass target controls.

## Setup

1. Start ARTEX with a recording proxy (the default Docker deployment includes it).
2. Go to **System → Settings** and enable **Traffic capture**.
3. In **Managed proxy pool**, add up to eight HTTP/HTTPS/SOCKS5 proxy URLs and
   acknowledge ownership/authorization for each.
4. Enter up to 64 **exact authorized target hostnames or IP addresses**, one per
   line, such as `internal-lab.example.com` or `192.0.2.10`. No schemes,
   ports, or wildcard patterns are allowed.
5. Enable the pool and save. Use **Check reachability** to run immediate probes.

The pool defaults to **off**. When enabled it is the upstream of ARTEX's
recording HTTP proxy. It takes precedence over the legacy single
`global_proxy` setting for recorded requests. Existing target approval
controls, the target noise governor, and the optional custom request identity
remain in place. Disabling traffic capture while the pool is enabled is rejected.

## Selection and failure semantics

- The pool probes each configured endpoint initially, every **5 minutes** and
  on demand. An HTTP proxy is checked for TCP reachability, HTTPS for a verified
  TLS handshake, and SOCKS5 for method negotiation (and credential authentication
  when configured). These checks **do not prove forwarding works** or contact
  target hosts.
- One *exact target hostname* remains pinned to its selected healthy upstream.
  This is host affinity, **not per-task session affinity**; different tasks
  visiting the same host use the same upstream while it remains healthy.
- After a failed health check, a subsequent connection can use a different
  healthy **authorized** upstream. Existing connections are not forcibly
  moved. ARTEX does not silently replay or retry requests on failover.
- If the host is not in the configured target allowlist, no upstream is
  healthy, or the pool configuration is unavailable, the recording proxy
  returns an error. **It never falls back to direct target connections**.
- Counters show HTTP requests *admitted/blocked* by the recording proxy,
  *upstream selection attempts*, and *binding changes*; they do
  not claim successful delivery. Health check times/latency reflect proxy
  endpoint reachability only.
- Pool selection works for requests passing through ARTEX's **recording proxy**.
  WebFetch with a configured proxy no longer retries direct on failure.
  Programs that bypass that proxy, such as raw scanners, independently proxied
  HTTP tools, or direct WebFetch paths, are outside this feature's routing
  guarantees. It does not replace network/firewall egress enforcement or
  written authorization for the target.

## Credentials

Upstream URLs with userinfo (for example `socks5://user:pass@host:1080`) are
stored encrypted using ARTEX's settings secrets key. The API masks credentials
and never returns raw authentication values. Editing an unchanged proxy
preserves its original credentials; replacing its URL replaces the credentials.

## API (authenticated admin routes)

- `GET /api/settings/proxy-pool`: configured nodes and live status, with
  credential-free display URLs.
- `PUT /api/settings/proxy-pool`: full configuration replacement. Example:

```json
{
  "enabled": true,
  "allowed_hosts": ["lab.example.test"],
  "proxies": [{
    "id": "internal-a",
    "name": "Owned lab gateway",
    "url": "socks5://proxy.internal:1080",
    "authorized": true
  }]
}
```

  For an existing saved node, `"url":"__masked__"` means "keep the stored URL".
- `POST /api/settings/proxy-pool/check`: check currently configured proxies
  immediately, returning updated reachability status.

## Verification

Run `go test ./managedproxy ./traffic ./server` for validation, host pinning,
fail-closed selection and credential handling. Server integration tests that
need PostgreSQL skip when `ARTEX_PG_DSN` is not configured. Run
`cd web && npm run build:static` to verify the UI.

**No public proxy-list fetch or tests against live external services are needed.**
