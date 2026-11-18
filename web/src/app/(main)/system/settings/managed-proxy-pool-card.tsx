"use client";

import * as React from "react";
import { NetworkIcon, PlusIcon, RefreshCwIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { ManagedProxyConfig, ManagedProxyStatus } from "@/lib/types";

const MASK = "__masked__";
type FormNode = { id: string; name: string; url: string; displayURL: string; hasCredentials: boolean; authorized: boolean };

export function ManagedProxyPoolCard({ trafficCapture }: { trafficCapture: boolean }) {
  const [status, setStatus] = React.useState<ManagedProxyStatus | null>(null);
  const [enabled, setEnabled] = React.useState(false);
  const [allowed, setAllowed] = React.useState("");
  const [nodes, setNodes] = React.useState<FormNode[]>([]);
  const [busy, setBusy] = React.useState(false);

  const apply = React.useCallback((s: ManagedProxyStatus) => {
    setStatus(s);
    setEnabled(s.enabled);
    setAllowed(s.allowed_hosts.join("\n"));
    setNodes(s.proxies.map(p => ({
      id: p.id, name: p.name,
      url: p.has_credentials ? MASK : p.url,
      displayURL: p.url, hasCredentials: p.has_credentials,
      authorized: p.authorized,
    })));
  }, []);

  const reload = React.useCallback(() => {
    api.managedProxyStatus().then(apply).catch(e => toast.error("Proxy pool: " + (e as Error).message));
  }, [apply]);

  React.useEffect(() => { reload(); }, [reload]);

  const update = (id: string, patch: Partial<FormNode>) =>
    setNodes(prev => prev.map(n => n.id === id ? { ...n, ...patch } : n));

  const save = () => {
    if (enabled && !trafficCapture) {
      toast.error("Enable Traffic capture above before activating the managed proxy pool.");
      return;
    }
    const cfg: ManagedProxyConfig = {
      enabled,
      allowed_hosts: allowed.split(/[\n,]/).map(x => x.trim()).filter(Boolean),
      proxies: nodes.map(n => ({
        id: n.id, name: n.name.trim(),
        url: n.url.trim(),
        authorized: n.authorized,
      })),
    };
    setBusy(true);
    api.setManagedProxyPool(cfg)
      .then(s => { apply(s); toast.success("Managed proxy pool saved. Health checks are running."); })
      .catch(e => toast.error("Save failed: " + (e as Error).message))
      .finally(() => setBusy(false));
  };

  const check = () => {
    setBusy(true);
    api.checkManagedProxyPool()
      .then(s => { apply(s); toast.success("Proxy reachability checks completed."); })
      .catch(e => toast.error("Health check failed: " + (e as Error).message))
      .finally(() => setBusy(false));
  };

  return (
    <Card className="mb-4 break-inside-avoid md:mb-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base"><NetworkIcon className="size-4"/>Managed proxy pool</CardTitle>
        <CardDescription>
          Add only proxies you operate or have explicit permission to use. The pool applies to traffic
          passing through ARTEX&apos;s recording proxy, never automatically imports public proxy lists, and
          restricts requests to the exact target hosts below. Health checks run every 5 minutes and on
          demand. A target stays on the same healthy proxy; failover occurs after a failed health check.
          No proxy available means requests fail, never connect directly. Existing noise limits, request
          signatures and target approvals still apply. Reachability checks do not establish end-to-end
          forwarding or target authorization.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="pool-enabled">Enable managed pool</Label>
          <Switch id="pool-enabled" checked={enabled} disabled={!status || busy}
            onCheckedChange={setEnabled} />
        </div>
        {!trafficCapture && (
          <p className="text-xs text-amber-600">Traffic capture is off. Turn it on above before enabling the pool.</p>
        )}
        <div className="space-y-2">
          <Label htmlFor="pool-allowed">Approved target hosts, one per line (exact hostname or IP)</Label>
          <Textarea id="pool-allowed" rows={3} value={allowed} disabled={busy || !status}
            onChange={e => setAllowed(e.target.value)}
            placeholder={"lab.example.org\n192.0.2.10"} />
          <p className="text-xs text-muted-foreground">No wildcards, protocols or ports. A target not on this list is blocked by the recording proxy while the pool is enabled.</p>
        </div>
        <div className="flex items-center justify-between gap-2">
          <Label>Authorized upstream proxies (up to 8)</Label>
          <Button variant="outline" size="sm" type="button" disabled={!status || busy || nodes.length >= 8}
            onClick={() => setNodes(prev => [...prev, {
              id: "managed-" + Math.random().toString(36).slice(2, 10),
              name: "", url: "", displayURL: "", hasCredentials: false, authorized: false,
            }])}>
            <PlusIcon data-icon="inline-start"/> Add proxy
          </Button>
        </div>
        {nodes.map(n => {
          const observation = status?.proxies.find(p => p.id === n.id);
          return (
            <div className="space-y-2 rounded-md border p-3" key={n.id}>
              <div className="flex gap-2">
                <Input aria-label="Proxy name" className="min-w-0" placeholder="Internal gateway A"
                  disabled={busy} value={n.name} onChange={e => update(n.id, { name: e.target.value })} />
                <Button variant="ghost" size="icon" type="button" aria-label={"Remove " + (n.name || "proxy")}
                  disabled={busy} onClick={() => setNodes(prev => prev.filter(p => p.id !== n.id))}>
                  <Trash2Icon className="size-4"/>
                </Button>
              </div>
              <Input aria-label={"Proxy URL for " + (n.name || n.id)} className="font-mono text-xs"
                value={n.url === MASK ? "" : n.url}
                placeholder={n.url === MASK ? "Saved credentials · enter new URL to replace" : "socks5://host:1080"}
                disabled={busy}
                onChange={e => update(n.id, { url: e.target.value, hasCredentials: false })} />
              {n.url === MASK && <p className="text-xs text-muted-foreground">Saved address: {n.displayURL} (credentials hidden)</p>}
              <div className="flex items-center gap-2">
                <Checkbox id={"owned-" + n.id} checked={n.authorized} disabled={busy}
                  onCheckedChange={v => update(n.id, { authorized: v === true })}/>
                <Label htmlFor={"owned-" + n.id} className="text-xs">I own or am explicitly authorized to use this proxy</Label>
              </div>
              {observation && (
                <p className="text-xs text-muted-foreground">
                  {observation.checked_at ? (observation.healthy ? "Reachable" : "Unreachable") : "Not checked"} ·
                  {observation.checked_at ? " checked " + new Date(observation.checked_at).toLocaleString() : " awaiting probe"} ·
                  {observation.selections} selections · {observation.failovers} failovers
                  {observation.healthy ? " · " + observation.latency_ms + " ms" : ""}
                </p>
              )}
            </div>
          );
        })}
        {nodes.length === 0 && <p className="text-sm text-muted-foreground">No proxies configured. The pool is off by default.</p>}
        <div className="flex flex-wrap gap-2">
          <Button onClick={save} disabled={!status || busy}>{busy ? "Working…" : "Save managed proxy pool"}</Button>
          <Button type="button" variant="outline" disabled={!status || busy} onClick={check}>
            <RefreshCwIcon data-icon="inline-start"/>Check reachability
          </Button>
          <Button type="button" variant="ghost" disabled={!status || busy} onClick={reload}>Reload</Button>
        </div>
        {status && <p className="text-xs text-muted-foreground">Live: {status.bound_hosts} sticky target bindings · {status.admitted_requests} HTTP requests admitted · {status.blocked_requests} blocked · {status.proxies.filter(p => p.healthy).length}/{status.proxies.length} reachable proxies. Counts reflect admission and upstream selection, not confirmed delivery.</p>}
      </CardContent>
    </Card>
  );
}
