"use client";

import * as React from "react";

import { FingerprintIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";

type HeaderRow = { id: number; name: string; value: string };

// This is a plain identification signal, not a secret-bearing auth header.
export function TargetIdentityCard() {
  const [userAgent, setUserAgent] = React.useState("");
  const [rows, setRows] = React.useState<HeaderRow[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const nextID = React.useRef(1);

  React.useEffect(() => {
    api.settings()
      .then((settings) => {
        setUserAgent(settings.target_user_agent ?? "");
        setRows(Object.entries(settings.target_headers ?? {})
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([name, value]) => ({ id: nextID.current++, name, value })));
      })
      .catch((e) => toast.error("Failed to load target identity: " + (e as Error).message))
      .finally(() => setLoaded(true));
  }, []);

  const updateRow = (id: number, change: Partial<HeaderRow>) => {
    setRows((prev) => prev.map((row) => row.id === id ? { ...row, ...change } : row));
  };

  const save = () => {
    const headers: Record<string, string> = {};
    const seen = new Set<string>();
    for (const row of rows) {
      const name = row.name.trim();
      if (!name || !row.value.trim()) {
        toast.error("Every custom header must have both a name and a value.");
        return;
      }
      if (seen.has(name.toLowerCase())) {
        toast.error("Duplicate header name: " + name);
        return;
      }
      seen.add(name.toLowerCase());
      headers[name] = row.value.trim();
    }
    setSaving(true);
    api.setSettings({ target_user_agent: userAgent.trim(), target_headers: headers })
      .then((settings) => {
        setUserAgent(settings.target_user_agent ?? "");
        setRows(Object.entries(settings.target_headers ?? {})
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([name, value]) => ({ id: nextID.current++, name, value })));
        toast.success("Target request identity saved");
      })
      .catch((e) => toast.error("Save failed: " + (e as Error).message))
      .finally(() => setSaving(false));
  };

  return (
    <Card className="mb-4 break-inside-avoid md:mb-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <FingerprintIcon className="size-4" />
          Target request identity
        </CardTitle>
        <CardDescription>
          Identify authorized ARTEX assessment traffic to services and their WAF/access logs. The configured
          User-Agent and custom headers are sent by ARTEX&apos;s built-in target HTTP clients and the recording
          proxy, even when Target noise is Off. Headers already set on requests are overridden by your signature.
          This is a global setting, not authentication. Avoid secrets. Direct scanner traffic that bypasses the
          recording proxy cannot be changed, and unproxied third-party tools may retain their own headers.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="space-y-2">
          <Label htmlFor="target-user-agent">User-Agent (optional)</Label>
          <Input
            id="target-user-agent"
            value={userAgent}
            disabled={!loaded || saving}
            onChange={(e) => setUserAgent(e.target.value)}
            maxLength={256}
            placeholder="ARTEX-Authorized-Assessment/1.0 (+security@example.org)"
            autoComplete="off"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <Label>Custom HTTP headers (up to 16)</Label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!loaded || saving || rows.length >= 16}
            onClick={() => setRows((prev) => [...prev, { id: nextID.current++, name: "", value: "" }])}
          >
            <PlusIcon data-icon="inline-start" /> Add header
          </Button>
        </div>
        {rows.length === 0 && <p className="text-xs text-muted-foreground">No custom headers configured.</p>}
        {rows.map((row) => (
          <div key={row.id} className="flex items-center gap-2">
            <Input
              aria-label="Header name"
              className="min-w-0 flex-1 font-mono text-xs"
              placeholder="X-Assessment-ID"
              value={row.name}
              onChange={(e) => updateRow(row.id, { name: e.target.value })}
              disabled={!loaded || saving}
              maxLength={128}
              autoComplete="off"
            />
            <Input
              aria-label="Header value"
              className="min-w-0 flex-[1.5] font-mono text-xs"
              placeholder="authorized-test-123"
              value={row.value}
              onChange={(e) => updateRow(row.id, { value: e.target.value })}
              disabled={!loaded || saving}
              maxLength={512}
              autoComplete="off"
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={"Remove " + (row.name || "header")}
              disabled={!loaded || saving}
              onClick={() => setRows((prev) => prev.filter((r) => r.id !== row.id))}
            >
              <Trash2Icon className="size-4" />
            </Button>
          </div>
        ))}
        <p className="text-xs text-muted-foreground">
          Use an organization-assigned marker such as X-Assessment-ID or X-Bug-Bounty. Authorization,
          Cookie, Host and connection-related headers are blocked for safety. Empty fields clear the signature.
        </p>
        <div>
          <Button onClick={save} disabled={!loaded || saving}>
            {saving ? "Saving…" : "Save request identity"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
