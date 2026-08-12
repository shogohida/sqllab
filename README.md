# sqllab

An interactive query-optimization sandbox: run a query unindexed, watch it
scan the table; add the suggested index; rerun the same query; watch the
plan flip to an index search. Every visitor gets their own private,
ephemeral SQLite database — nothing you do here affects anyone else, and
nothing persists after your session goes idle.

A second panel turns a plain-English question into SQL using a small
language model running **entirely inside your browser** via
[WebLLM](https://github.com/mlc-ai/web-llm)/WebGPU — no server-side model,
no API key, no data leaves your machine.

```
sqllab/
├── internal/db/         schema, deterministic dataset generation, and the
│                         statement guard every query — typed or AI-generated — passes through
├── internal/session/     per-visitor ephemeral in-memory database + eviction
├── internal/scenarios/   the 4 canned slow-query/fix pairs
├── internal/api/         HTTP handlers
├── web/                  embedded frontend (no build step)
└── cmd/server/           entrypoint: seeds the dataset once, serves everything on one port
```

## Why this project

A past resume line — "improved dashboard response time by 40% through load
profiling and MySQL index optimization" (Zeroboard) — is a real number, but
it's still just a sentence on a page. This project makes the underlying
mechanism something a visitor can trigger themselves: pick a realistic
slow query, watch `EXPLAIN QUERY PLAN` show a full table scan and a
multi-hundred-millisecond wall-clock time, add the exact index that fixes
it, rerun, and watch the plan and the clock both change. It's the
reproducible version of the claim, not a restatement of it.

The AI panel exists because generative-AI experience is a second thing
worth demonstrating, and because it forced an interesting constraint: a
public demo can't safely make server-side calls to a metered LLM API (see
"Design decisions" below) — so the natural-language-to-SQL feature had to
run somewhere that costs nothing no matter how many strangers click it.

## Live demo

Deployed for free on [Render](https://render.com) via `render.yaml` — no
Docker, no credit card required for the free plan. Connect this repo and
Render builds `cmd/server` automatically. Free-tier services sleep after 15
minutes of inactivity; the first request after a sleep takes a few seconds
to wake up.

```bash
go run ./cmd/server
# open http://localhost:8080
```

## Design decisions & trade-offs

**Why the AI feature is entirely client-side.** A public demo that calls a
metered LLM API (Claude, OpenAI) on every visitor's request either exposes
a real API key to unbounded abuse or requires spending-cap plumbing that's
still a live cost risk. Running a small model
(`Qwen2.5-1.5B-Instruct`, resolved dynamically from WebLLM's model registry
rather than hardcoded, since that list changes) via WebGPU in the visitor's
own browser sidesteps the problem entirely: the server never sees the
question, never calls a paid API, and the cost of inference is paid by the
visitor's own GPU, not by this project's hosting bill. The trade-off is
honest: a 1.5B-parameter model is noticeably weaker than Claude at SQL
generation, and WebGPU isn't available on every browser (Safari, most
mobile browsers) — the panel feature-detects `navigator.gpu` and disables
itself with an explanation rather than failing silently, and the core
query/index demo works fully without it either way.

**AI-generated SQL is never trusted more than typed SQL.** Whatever the
model outputs is POSTed to the same `/api/query` endpoint and passes
through the identical `internal/db/guard.go` validation as anything a
visitor types by hand — an allowlist of `SELECT` / `EXPLAIN QUERY PLAN` /
`CREATE INDEX` / `DROP INDEX`, single-statement enforcement, a
column/table whitelist for index DDL, a blocklist for `ATTACH`/`PRAGMA`
as defense-in-depth, and a 3-second execution timeout. The origin of a SQL
string says nothing about whether it's safe to run.

**Per-visitor isolation over a shared dataset.** Each session gets its own
`:memory:` SQLite database, seeded by `ATTACH`ing a shared read-only
template file and bulk-copying its tables (well under 100ms) rather than
regenerating ~200K rows per visitor. This means one visitor's added index
or dropped index never leaks into anyone else's demo, and there's no
shared mutable state to corrupt. Idle sessions are evicted after 10
minutes and a hard cap (20 concurrent sessions) protects the free tier's
512MB RAM budget — see `internal/session/store.go`.

**Dataset size is deliberately modest.** `orders` (50,000 rows) and
`order_items` (150,000 rows) are small enough that ~20 concurrent
in-memory copies fit comfortably in 512MB, and Render's free tier's
throttled CPU (0.1 vCPU) is what actually makes an unindexed scan feel
slow at this scale — not sheer row count. No exact millisecond numbers are
promised in the UI copy; shared-CPU performance varies, and showing the
relative before/after is the defensible framing.

**SQLite, even though the original claim was MySQL-specific.** The résumé
line this project reproduces is literally about MySQL index optimization,
and MySQL's `EXPLAIN`/`EXPLAIN ANALYZE` (8.0.18+) genuinely shows more than
SQLite's `EXPLAIN QUERY PLAN` does: actual per-step row counts and timing,
`key_len` (how much of a composite index was actually used), a `filtered`
estimate, and an `Extra` column (`Using filesort`, `Using temporary`,
`Using index condition`) with no SQLite equivalent. That gap is real and
left visible rather than papered over — it's not solvable without giving
up the property the rest of this section depends on: an embeddable,
single-process database that a free web service can seed and throw away
per visitor in under 100ms with zero infrastructure cost. MySQL's
client-server model doesn't have that property at any price point (see
the live demo's hosting constraints above). What *does* carry over
one-to-one is the mechanism itself — a missing composite index turning a
scan into a search is the same fix, and the same reasoning, in InnoDB.

## Running the tests

```bash
go test ./... -v
```

`internal/db/guard_test.go` covers the statement allowlist/blocklist
(including that AI-shaped inputs like stacked statements and `ATTACH` are
rejected); `internal/session/store_test.go` covers session creation,
capacity enforcement, and idle eviction; `internal/scenarios` asserts every
canned scenario's query and suggested index actually pass the guard, so a
scenario typo fails CI instead of silently breaking the demo;
`internal/api/handlers_test.go` exercises the full HTTP flow with
`httptest`, including auto-provisioning a session on first request.

## Known limitations / roadmap

- **No server-side LLM provider yet.** `ecocopilot` (a sibling project)
  established an `LLM_PROVIDER=ollama|claude|openai` pluggable pattern;
  mirroring that here — off by default, opt-in via env var for someone
  running this locally with their own API key — would let a visitor compare
  the free in-browser model's SQL quality against Claude's. Deliberately
  left out of the public demo to avoid reintroducing the cost/abuse
  surface the WebLLM approach was chosen to avoid.
- **In-memory sessions don't survive a process restart.** Acceptable for a
  demo (Render's free tier sleeps after inactivity anyway); a real product
  would need durable storage, which is explicitly not the point here.
- **The AI panel needs WebGPU.** No fallback to a smaller CPU-only model
  yet for browsers without it (mainly Safari and mobile).

## What each package proves

| Package | Demonstrates |
|---|---|
| `internal/db` | Understanding of *why* an index changes a query plan, not just that it does — deterministic dataset generation, `EXPLAIN QUERY PLAN` parsing, and a security-conscious statement validator |
| `internal/session` | Resource-bounded multi-tenant isolation on a memory-constrained host: capacity limits, idle eviction, no shared mutable state |
| `web/app.js` | Client-side LLM integration (WebLLM/WebGPU) with graceful feature detection — generative-AI experience without a server-side cost or security surface |
| `internal/api` | A public HTTP surface that treats every input — human or AI-generated — as equally untrusted |
