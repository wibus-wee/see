# Lane E integration notes

Lane E owns `internal/search`, `internal/regexplayground`, `internal/fileobs`,
the fuzz infrastructure, and the threat model. This document is the
interface between that work and what the other lanes own: what they can
consume today, what Lane E deliberately did not implement, and what the
integration phase must wire. Nothing here is implemented elsewhere yet —
each section is a contract proposal against code the owning lane controls.

## §A Three-stage API — assessment only (not implemented, per dispatch)

**Current state.** The codebase is in better shape than the spec's worry
assumes, because the typed-options pattern already exists where it matters
most:

- `internal/search` exposes exactly one structured entry point:
  `search.Options` + `search.Run`/`search.Args`. `internal/app/find.go:118`
  is the single construction site; the UI (find panel, checkboxes, query
  transformations) only mutates the one `Options` value. There is no
  parallel parameter-assembly path. REQ-API-010/020/030 are effectively
  satisfied for search: the "simple call" is an `Options` zero value plus
  a query.
- `search.ReplaceFile` is single-shape: matches + replacement, no
  per-callsite assembly.
- The save path (`saveDoc`) has no options struct at all — it is one
  behavior, which is simpler than three-stage and fine until a second
  caller with different needs appears.

**Genuine gaps** (for the integration phase, not urgent):

- **git operations** are the one place where multiple UI entries plausibly
  assemble overlapping invocation shapes (working-copy restore vs.
  checkout vs. discard). Worth a typed-options review once Lane A's
  external-operation pipeline settles — and worth doing *then*, because
  the options shape should mirror that pipeline's vocabulary, not today's.
- **LSP requests** are per-method typed already (the protocol types are
  the options); nothing to do.

**Recommendation.** Do not build a generic three-stage framework. When the
second caller of an operation appears with divergent needs, convert that
operation to the `search.Options` shape: one struct, all entry points
construct it, one validation boundary. The spec's REQ-API-040 (uniform
fail-fast) falls out of that shape automatically. This is SPEC-009's own
P2 assessment — "随外部操作复杂度增长再统一，不必先抽象" — and the
evidence supports it.

## §B FileObservation — the `fileobs` package and its wiring contracts

`internal/fileobs` exists and is tested (`internal/fileobs/fileobs.go`,
`digest.go`, `fileobs_test.go`). It provides:

- `Observation` — immutable snapshot: exists/absent, source, observed-at,
  presence-tracked size/mtime/SHA-256/ETag/version, three-state
  current/deleted. Construction copies; nothing mutates.
- `Check(expected, current)` — the conditional-operation primitive:
  nil / `ErrConditionMismatch` / `ErrIncomparableIdentity`.
- `Stale`, `DigestSHA256`, `DigestFileSHA256`, `VerifyDigest`,
  `VerifyContentDigest` — integrity primitives.

### Wiring contract for the save path (owner: Lane C / app integration)

Today `saveDoc` (`internal/app/editing.go:477`) implements a *content*
condition — it re-reads the file and compares with `d.diskText` before
writing. That is stronger than an mtime comparison but still not the spec
shape: the check and the write are two separate system calls, so a writer
racing between them lands its bytes and then has them silently overwritten
(REQ-COND-050 violated in the small window).

The wiring contract, once Lane C's atomic write primitive exists:

1. `d.diskText`'s snapshot becomes an `Observation{source: SourceRead}`
   enriched with `WithDigest` — note the digest is over the *on-disk
   bytes* (including BOM), not the decoded text.
2. Save becomes: stage buffer → at commit, evaluate
   `fileobs.Check(expected, fileobs.ObservePath(SourceRead, path))` —
   i.e., the check inside the same atomic-commit boundary as the write,
   per REQ-COND-050/060.
3. `ErrConditionMismatch` surfaces as the existing conflict UI (no UX
   change needed — the type only makes the error category correct and
   testable).
4. **Important semantic note**: the content comparison `saveDoc` does
   today is stronger than size+mtime. Any migration to observation-based
   checking must use the digest attribute, not regress to mtime — mtime
   granularity and clock skew make it strictly weaker than what users
   have now.

### Wiring contract for the watcher (owner: Lane B / app)

The watcher currently invalidates on any event (`watch.go` sets
`quick.files = nil`). A `SourceWatch` observation per file would let
reload decisions distinguish "mtime changed but content identical"
(editor tools that rewrite files atomically without semantic change) from
real changes. Not urgent; the current behavior errs toward reloading,
which is safe. Integration phase: evaluate after Lane B's watcher
incremental-merge work, since the observation lifetime must match how
increments merge.

### Wiring contract for git status (owner: Lane B / app)

Git already computes its own content identity (blob SHA). `SourceGit`
observations should carry git's own SHA verbatim in the `version`
attribute rather than re-hashing — REQ-META-030 says backend tokens are
kept verbatim, and git's SHA-1/SHA-256 is exactly such a token. Do not
translate it into the SHA-256 digest attribute; they are different
contracts.

### Wiring contract for local history (owner: Lane C)

`SourceHistory` observations bind a snapshot record to when it was
captured. The more valuable integration is integrity, see §D below.

## §C Conditional operations — contract with Lane C's atomic write

What Lane E provides: `fileobs.Check` and the two error categories. What
Lane E deliberately did **not** do: touch the save path, because the
condition must be evaluated inside the publish boundary Lane C is
building, and a check added now at the wrong boundary would be false
evidence of REQ-COND-050 compliance.

The contract the integration phase must satisfy:

1. **Evaluation point**: conditions evaluate at commit/publish, never at
   stage/begin (REQ-COND-060). For a single-file save that means: the
   atomic-write primitive receives the expected `Observation`, re-observes
   inside the same critical section as the rename, and refuses with
   `ErrConditionMismatch` before the rename happens.
2. **Best-effort honesty**: POSIX cannot make observe+rename truly atomic
   without kernel support. The honest degradation is a small-window race
   documented in the contract — REQ-COND-070 requires exactly this
   honesty: advertise "the window is the rename syscall" rather than
   claiming atomicity. Even so, moving the check adjacent to the rename
   shrinks the current window (read entire file → compare → write entire
   file) by orders of magnitude.
3. **Batch independence** (REQ-COND-080): workspace replace already
   reports per-file results (`find_replace.go`); the integration must
   keep per-entry outcomes independent — one file's mismatch aborts only
   that file.
4. **Error taxonomy**: `ErrConditionMismatch` must not collapse into the
   generic save-failure note; the conflict UI already exists, it just
   needs the typed error to trigger on.

## §D Checksum — what exists, what integration must add

**Exists today (verified, tested):**

- ripgrep install: fetch → checksum verify → extract validate → stage →
  atomic rename. Mismatch rejects before anything is written; existing
  installs are never overwritten (refuse-first design); truncated archives
  reject. See `internal/app/tools_test.go` — REQ-CHK-020/030 evidence is
  complete for this surface.
- gopls install: delegates verification to the Go module checksum
  database through `go install` with a pinned version. This is a
  documented trust delegation (see SECURITY-THREAT-MODEL.md), not a gap.

**Missing — for Lane C's local history work:**

`internal/localhistory` records carry no content digest. Structural JSON
corruption is detected (the record fails to load), but *semantic*
corruption — valid JSON, wrong bytes — is silently restorable today. The
`snapshotID` includes a truncated SHA-256 of content but functions as an
identifier, not a verified integrity check: nothing recomputes it at
restore time.

Integration contract:

1. Lane C's store migration adds a full-content `digest` field per
   snapshot (use `fileobs.DigestSHA256` / hex encoding — the field must
   be the full hex string, `fileobs.ParseHexDigest` rejects wrong shapes
   fail-closed).
2. Restore re-computes the snapshot content's digest and compares before
   offering the snapshot as restorable; a mismatch is
   `fileobs.ErrDigestMismatch` and the snapshot is *reported as corrupt*,
   not silently dropped and not restored.
3. Because restore in See applies through the editor buffer rather than
   blind file overwrite, the REQ-CHK-030 "target untouched" property is
   structurally satisfied — the evidence needed is only that corrupt
   snapshots never reach the buffer.

**Range reads** (REQ-CHK-050): See's hex viewer and paging read partial
file content; no full-object checksum is or should be applied there —
the spec's requirement is only that partial reads never *claim* full-object
verification, which they don't. No action needed; documented here so the
decision is explicit rather than accidental.

## §E Fuzz — targets owned by other lanes

Full input shapes, budgets, and invariants are in `lane-e-fuzzing.md`
("Interface contracts for targets owned by other lanes"). Summary of the
wiring order recommended for the integration phase:

1. **LSP frame parser** — highest value: it parses attacker-influenced
   bytes off a socket in a background goroutine, and the threat model
   claims no-panic on malformed frames without fuzz evidence yet.
2. **Path normalization / confined join** — small, self-contained, and
   the "never escapes root" invariant is crisply testable.
3. **Local history format** — pair with the §D digest work so corruption
   detection has adversarial-input evidence.
4. Hex paging math and edit-sequence oracles — valuable but lower risk.

## §F Security findings beyond Lane E's ownership

Verified during threat-model review; recorded here rather than fixed
because the files belong to other lanes:

1. **Server stderr passes verbatim into logs** (`internal/lsp/client.go`
   `tail.Write`). This is documented in the threat model as the server's
   own speech. It is the correct design — but `window/logMessage` and
   stderr both mean a *compromised* (not malicious-workspace, but
   compromised-upstream) server can flood or inject misleading text into
   the log panel. The 1000×8192 ring bounds the memory; the UI impact is
   accepted. No change recommended; the boundary is documented.
2. **No redaction layer exists anywhere** (verified by grep). Today this
   is correct *because* See never logs the secret-shaped inputs it holds
   (env values, command arguments) — verified by behavioral test. The
   risk is future drift: any future logging of process arguments or
   environment would bypass review. Consider, at integration time, a
   lint or a narrow logging-helper contract rather than a redaction
   layer: redaction after the fact is strictly weaker than never logging.
3. **`saveDoc`'s non-atomic check-then-write** — see §C. It is the most
   consequential correctness gap adjacent to security (silent overwrite
   of a concurrent writer's bytes), but its fix belongs to Lane C's
   pipeline.

## Cross-lane repair suggestions (not implemented — owner decision)

| Finding | Owner | Suggestion | Severity |
| --- | --- | --- | --- |
| Save check/write race window | Lane C | Move condition into atomic commit; see §C | high correctness value |
| localhistory semantic corruption undetectable | Lane C | content digest at restore; see §D | medium |
| LSP frame parser has no fuzz coverage | Lane A | wire target per lane-e-fuzzing.md | medium |
| No regression defense against future secret logging | shared | logging-helper contract or lint | low — preventive |
