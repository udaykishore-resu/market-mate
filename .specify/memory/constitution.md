# MarketMate Constitution

**Version**: 1.0.0
**Ratified**: 2026-08-14

The non-negotiable engineering principles for this project. Every spec, plan, and
implementation task is checked against this document. A plan that violates a
principle must either be revised or record an explicit, justified exception in
its Complexity Tracking section.

MarketMate depends on three paid third-party APIs. Almost everything below
follows from taking that seriously: a product that cannot run without someone
else's billing account is a product that cannot be demonstrated, cannot be
tested, and therefore cannot be trusted.

---

## Principle I — Spec Before Code

No implementation work begins without a written specification stating user value
in plain language, with acceptance scenarios in Given/When/Then form. Specs
describe *what* and *why*; plans describe *how*. A spec naming a library, a
framework, or an API vendor has leaked implementation detail and must be
rewritten.

## Principle II — It Must Run With Nothing

The service starts and serves requests with an entirely empty environment. No
API key, environment variable, config file, or provisioned account is required
to reach a working demonstration of every feature.

Rationale: the previous build exited at startup unless three paid keys were
present. That single fact is why its URL parser could panic on `""` and why its
store lookup shipped a hardcoded San Francisco coordinate — nobody, including
CI, could run the thing to notice.

Consequence: every external capability has a fixture implementation alongside
its live client, and selection happens per capability at startup.

## Principle III — Simulated Data Is Always Labelled

A response containing fixture data is never presentable as a live result. The
API carries explicit provenance naming which stages were simulated, the UI
surfaces it, and the health endpoint reports the mode.

Rationale: Principle II is only safe if it cannot mislead. A demo that silently
looks like production is worse than no demo, because someone will screenshot it
and call it evidence.

## Principle IV — External Calls Are Bounded and Cancellable

Every outbound request carries a context derived from the inbound request and a
timeout. No `context.Background()` in a request path, no unbounded
`http.Client`. A client disconnect cancels the work it was waiting on.

Rationale: three sequential third-party calls per request is three chances to
hang forever while holding a connection.

## Principle V — Untrusted Input Cannot Crash the Service

Anything arriving from a user — above all the URL field, which is the entire
front door of this product — is parsed defensively. No slicing without a bounds
check. Parsers return an error rather than a plausible-looking wrong answer, so
callers can distinguish "invalid" from "valid but empty".

Every parser carries table-driven tests over the real input space plus a fuzz
target. Two regressions are permanently pinned: the empty string, and any input
shorter than a video ID.

## Principle VI — The Handler Depends on Interfaces

HTTP handlers depend on narrow interfaces, never on concrete clients that build
network connections in their constructors.

Rationale: this is the seam that makes Principles II, III, and V testable at
all. When the handler held concrete `*VideoService` pointers, there was no way
to instantiate it in a test — which is the root cause of every defect this
constitution exists to prevent. Tests run the real handler and the real routing
against fixtures, with no network and no keys.

## Principle VII — Never Guess the User's Location

Location is resolved from evidence or declared as an estimate. A fallback
coordinate is permitted, must be documented, and must be flagged to the user as
a fallback in the response.

Rationale: "nearby stores" two thousand miles away is worse than no feature, and
silently defaulting is how that ships.

---

## Governance

Amendments require a version bump here and a note in the changelog of the
affected specs. Principles II and VI are load-bearing for the project's
testability; amending either means rewriting the affected plans, not just the
code.
