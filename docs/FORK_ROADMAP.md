# ARTEX fork roadmap

> Planning document for [mateocabanal/ARTEX](https://github.com/mateocabanal/ARTEX).
> Status: proposals only; none of these features is implemented by this document.
> Based on BaaRai/ARTEX `main` at `56d11bb51caf8118c0d7a462b2b7e623f4a41e2f` (2026-11-17).
> License: AGPL-3.0. Retain upstream attribution and applicable source-availability obligations.

## Product goal

A self-hosted, evidence-first **authorized security assessment console** optimized for a
mixed local/cloud LLM fleet. Preserve upstream's dual-graph model, proof-linked findings,
MCP support, and existing approval/recording controls. Add stronger deterministic control,
reliable orchestration, better model scheduling, and operator interaction.

## Guardrails / non-goals

- **Authorized targets only.** Each engagement requires a recorded owner/authorization,
  explicitly approved domains/IPs/ports, and an expiration time. No testing outside that scope.
- Enforce policy in the network/tool execution layer, not only prompts. Fail closed on scope
  validation, redirect and DNS/IP changes, tool bypass attempts, or unavailable policy checks.
- Run workers inside isolated, constrained containers; scrub credentials, PII and session
  cookies from logs/reports. Audit every approval, denial, tool execution and policy decision.
- No features intended to bypass target controls, conduct indiscriminate internet scanning,
  or silently test third-party systems.
- Do not deploy this fork to production or run scans until its authorization gates and
  isolation behavior are tested in controlled labs.

## P0 — safe baseline (implement first)

1. Pin a known-good upstream baseline, record source/provenance, and keep `upstream` as a
   separate Git remote. Create CI for Go checks, UI checks, dependency scans and basic
   configuration/security tests. Do not assume upstream `main` is a stable release.
2. Make engagement authorization explicit and mandatory. Default-deny destination policy
   should cover native HTTP, proxy traffic, browser, external scanners and Bash-launched
   network tools; test redirects, DNS rebinding, direct-IP access and egress bypasses.
3. Add Docker-based local test fixtures, a no-network/offline replay mode, and seeded
   regression scenarios. Never point automated CI at unrelated public targets.

## P1 — distinct fork capabilities

### Adaptive model routing: Strata + Basalt + external providers
Upstream already supports LLM profiles, failover and round-robin worker rotation. Extend
rather than duplicate them:
- Per-role model capabilities (planner, worker, retester, reporter), tool-call reliability,
  context limits, provider health, token cost, expected latency and queue depth.
- Route tasks by capability and budget rather than fixed A/B cycles; support local
  OpenAI-compatible endpoints, with Strata/Basalt-specific telemetry adapters where needed.
- Respect accelerator occupancy and shared inference service concurrency limits.
- Benchmark route decisions against a fixed evaluation corpus and show the selected model,
  decision reasons, tokens, cache hits, wall time and cost.

### Durable agent control plane
- PostgreSQL-backed job/intent leases, idempotent dispatch, bounded work queues, recovery
  after restart, and persisted token/turn/elapsed-time budgets.
- Operator-visible suspend/resume/cancel, clear timeouts, replay and per-run provenance.
- A deterministic approval/authorization policy independent from the LLM.

### Agent Inbox integration
- Send structured, actionable requests for target authorization, tool approvals,
  follow-up questions and finding review to an operator's phone/watch.
- Include evidence summaries, scope, action type, expiration, and approve/reject/clarify
  results. Use authenticated, one-use approval tokens with full audit trails.
- Pending/expired/denied approval must never be interpreted as approval.

## P2 — knowledge, evidence and operator experience

### GBrain bridge
- Engagement-scoped retrieval with explicit provenance and source permissions.
- Store only reviewed, durable findings/lessons; filter secrets and customer data.
- Detect contradictions and superseded lessons. Keep read/write capabilities separate.

### Evidence and report workflow
- Content-addressed evidence bundles, hashes, selective redaction, reproducible
  offline replay, reviewer annotations, retest comparison and provenance checks.
- Export Markdown, PDF and machine-readable JSON/SARIF for defensive remediation.
- Keep evidence disclosure scoped to the relevant engagement.

### Fleet dashboard
- Rosé Pine operator view with agent timeline, graph coverage, queue wait, model routing,
  token/cache metrics, running costs and requested approvals.
- Optional links to existing Portainer/Fleet monitoring without embedding secrets.
- A read-only timeline replay that answers *why did the planner choose this intent?*

### Evaluation harness
- Authorized test fixtures (e.g. deliberately vulnerable local apps), deterministic
  replay datasets, prompt-injection resilience cases, and a false-positive adjudicator.
- Score verified finding quality, coverage, completion rate, time, tokens, model
  efficiency and policy violations across commits.

## P3 — later extensions

- Multi-host worker pool with per-node capabilities, short-lived credentials and
  lease-based scheduling across trusted machines.
- Versioned plugin hooks layered on existing MCP support; per-plugin scopes and audit logs.
- Separate organizations/engagement workspaces, RBAC and vault integration.
- A workflow template registry for *authorized* assessment and remediation pipelines.

## Suggested milestones

**M0 — fork foundation:** verified GitHub fork, clone, remotes, documentation (this file).
**M1 — safety baseline:** permissioned engagements, fail-closed egress enforcement,
isolated lab fixtures and CI.
**M2 — model routing:** Strata/Basalt provider tests, health telemetry and adaptive
scheduler with measured comparison to upstream rotation.
**M3 — human-in-the-loop:** Agent Inbox structured approvals, expiry and mobile review.
**M4 — reliability/knowledge:** durable execution leases and GBrain scoped retrieval.
**M5 — release quality:** evidence exports, dashboard and reproducible evaluations.

Keep patches small and isolated, upstream changes regularly, and prefer backward-compatible
migrations and feature flags. Start on separate feature branches with tests and review.
