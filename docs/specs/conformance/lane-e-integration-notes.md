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
  nil / `ErrConditionMismatch` / `ErrIncomparableIdentity`. Fail-closed:
  an attribute the expectation carries but the current observation does
  not measure is incomparable, never a silently weakened comparison.
  The expectation fixes the condition's strength; extras in the current
  observation are ignored.
- `ObserveContent` — `ObservePath` with the content digest measured
  atomically with the stat: the strong-observation constructor the save
  wiring uses. `ObservePath` alone is a weak observation.
- `Stale`, `DigestSHA256`, `DigestFileSHA256`, `VerifyDigest`,
  `VerifyContentDigest` — integrity primitives.

### Wiring contract for the save path (owner: Lane C / app integration)

Today `saveDoc` (`internal/app/editing.go:477`) implements a *content*
condition — it re-reads the file and compares with `d.diskText` before
writing. That is stronger than an mtime comparison but still not the spec
shape: the check and the write are two separate system calls, so a writer
racing between them lands its bytes and then has them silently overwritten
(REQ-COND-050 violated in the small window).

The wiring contract, once Lane C's atomic write primitive exists —
**instructions for the Lane C integrator, load-bearing in every step**:

1. `d.diskText`'s snapshot becomes an observation built by
   `fileobs.ObserveContent(SourceRead, path)` at load time (and rebuilt
   the same way after every successful save) — never plain
   `ObservePath`. Note the digest is over the *on-disk bytes*
   (including BOM), not the decoded text.
2. Save becomes: stage buffer → at commit, evaluate
   `fileobs.Check(expected, observed)` where `observed` comes from
   `fileobs.ObserveContent(SourceRead, path)` — i.e., the check inside
   the same atomic-commit boundary as the write, per REQ-COND-050/060.
   **The re-observation must be `ObserveContent`, not `ObservePath`.**
   `ObservePath` is a weak observation: it carries size+mtime and never
   the digest. Since the respin of this contract, `Check` fails closed
   on exactly that mistake — an expectation carrying the digest checked
   against a digest-less observation is `ErrIncomparableIdentity`, not
   a silently weakened size+mtime comparison — so literal-minded wiring
   with `ObservePath` fails loudly at the first save rather than
   shipping the regression. Do not "fix" that error by weakening the
   expectation; fix it by re-observing with the digest.
3. `ErrConditionMismatch` surfaces as the existing conflict UI (no UX
   change needed — the type only makes the error category correct and
   testable). `ErrIncomparableIdentity` is a wiring bug and must never
   reach the UI as a file conflict: if it appears, the wiring deviated
   from steps 1–2.
4. **Important semantic note**: the content comparison `saveDoc` does
   today is stronger than size+mtime. Any migration to observation-based
   checking must use the digest attribute, not regress to mtime — mtime
   granularity and clock skew make it strictly weaker than what users
   have now. This is why both ends of the condition are digest-carrying
   observations: a digest-less expectation makes `Check` evaluate a
   weak condition *correctly and silently* — no strictness in `Check`
   can detect an expectation that was never strong.

Why the contract is written this way (the failure it is designed
against): independent verification demonstrated that wiring step 2
literally with `ObservePath` — the shape this section originally showed
— regressed save-conflict detection from content comparison to mtime
comparison: a same-length content replacement with the mtime restored
passed the check, because the comparison silently dropped every
attribute the re-observation did not carry. Two layers now prevent it:
the contract prescribes `ObserveContent` at both ends, and `Check`
refuses to evaluate any condition whose expectation attributes the
current observation does not measure. The first layer is the design;
the second exists because a contract a type system does not enforce is
a contract that depends on the reader's memory.

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
   commit closure passed to Lane C's atomic-write primitive
   (`atomicfile.CommitIf`) re-observes with
   `fileobs.ObserveContent(SourceRead, path)` — the digest-carrying
   observation, see §B — evaluates `fileobs.Check` against the
   expectation, and refusal with `ErrConditionMismatch` happens before
   the rename. Lane C's `cond func() error` shape leaves the
   re-observation to the caller, so the digest discipline in §B lives
   entirely in this closure: there is no second place that enforces it.
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

## §F Security decisions beyond Lane E's ownership

Recorded here rather than implemented because the files belong to other
lanes:

1. **Server stderr is untrusted input and must be redacted — cross-lane
   decision, resolved.** Lane E originally streamed server stderr into
   the log verbatim (`internal/lsp/client.go` `tail.Write`), documented
   as the server's own speech. That position contradicts the threat
   model's own in-scope item 3 — server configuration env carries real
   tokens, and a crashing or compromised server can echo them — and
   Lane A independently built the opposite: `proc.Tail` redacts stderr
   against the process environment as bytes arrive (secret-named
   variables, long token-shaped values, URL userinfo passwords;
   split-write-safe, so a secret straddling pipe reads still completes).
   **Resolved in favor of Lane A's position**: server stderr is
   attacker-influenced content that may carry secrets, and it enters
   See's log redacted. Integration wiring:
   - Lane A's streaming redactor is the **only** redaction layer —
     route server stderr through it exactly once, at the tail before
     the log write. See's own logging must not bypass it, and no
     second redactor may be stacked on top: layered redaction is how
     redaction gaps hide.
   - Nothing else changes in the boundary: the log panel remains a
     bounded in-memory ring, and stderr text is still never interpreted
     as commands or markup.
2. **The redactor complements, never replaces, not-logging.** See's own
   speech must continue to never format secret-shaped inputs (env
   values, command arguments) — verified by
   `TestConfiguredServerSecretsStayOutOfTheLog`. Redaction after the
   fact is strictly weaker than never logging: it protects against the
   server's speech and future drift, not as a license to log argv/env.
3. **Case-variant paths evade lexical workspace containment** — verified
   defect, fixed in Lane A's lane, not here. On case-insensitive volumes
   (default macOS APFS) `EvalSymlinks` does not normalize case, so a
   case-variant spelling of a workspace path names the same bytes while
   comparing lexically outside the workspace: the workspace-binary
   denial in `languages.FindConfigured` (and the same lexical shape in
   `search.FindRipgrep`) is defeated end-to-end. Lane A fixed the same
   defect in `proc.ValidateExecutable` with `diskSpelling` — per-component
   `os.SameFile` probing, mirroring Lane B's `workspace.DiskSpelling` —
   on its own branch. Lane E deliberately does not re-implement it (two
   implementations of one primitive would collide at merge). The
   integration checklist below carries the coverage check.
4. **`saveDoc`'s non-atomic check-then-write** — see §C. It is the most
   consequential correctness gap adjacent to security (silent overwrite
   of a concurrent writer's bytes), but its fix belongs to Lane C's
   pipeline.

## §G Integration checklist

Load-bearing checks the integration phase must confirm before the merged
state ships. Each one is cheap to verify and expensive to discover later.

1. **Save wiring uses digest-carrying observations at both ends** — §B
   steps 1–2 verbatim: `ObserveContent` at load/post-save for the
   expectation and at commit for the re-observation. If any
   `ErrIncomparableIdentity` reaches the save path, the wiring deviated;
   do not paper over it.
2. **Lane A's `diskSpelling` (or Lane B's `workspace.DiskSpelling` they
   converge into) covers every executable resolver**, not just
   `proc.ValidateExecutable`: `languages.FindConfigured` and
   `search.FindRipgrep`'s workspace exclusion have the same lexical
   shape and must compare in the disk's spelling too. Until then the
   threat model's workspace-binary invariant is provisional — the
   document says so explicitly, and this check is what un-provisions it.
3. **Server stderr flows through Lane A's redactor exactly once** — the
   single redaction layer, per §F item 1. Verify with a server that
   echoes its own environment: the token must not appear in the log
   panel, and `TestConfiguredServerSecretsStayOutOfTheLog` must still
   pass unmodified (it tests See's own speech, which the redactor does
   not excuse).
4. **One `diskSpelling` implementation survives the merge.** Lane A's
   NOTE says it mirrors Lane B's; keeping both is a fork of a security
   primitive. Whichever survives, the loser's callers move in the same
   integration wave.

## Cross-lane repair suggestions (not implemented — owner decision)

| Finding | Owner | Suggestion | Severity |
| --- | --- | --- | --- |
| Save check/write race window | Lane C | Move condition into atomic commit; see §C | high correctness value |
| localhistory semantic corruption undetectable | Lane C | content digest at restore; see §D | medium |
| LSP frame parser has no fuzz coverage | Lane A | wire target per lane-e-fuzzing.md | medium |
| Server stderr enters the log unredacted today | Lane A (mechanism) / integration (wiring) | resolved: wire Lane A's redactor as the single layer; see §F item 1 | medium — wiring is the deliverable |
| Case-variant paths evade workspace-binary denial | Lane A/B (mechanism) / integration (coverage) | extend disk-spelling comparison to all resolvers; see §G item 2 | medium — provisional until covered |
