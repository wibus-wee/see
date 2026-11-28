# Re-verification of Lane E — SPEC-009 round 2, respin

- Re-verifier: independent re-verification engineer, second dispatch (the
  first re-verify session died to infrastructure failure before producing
  any state; every result below was re-derived in this session)
- Target: branch `spec-lane-e-round2` fixes, tip `d5d0dd3` — three commits
  over the verified base `e593858`:
  - `5bb38f4` fix(fileobs): fail closed when the current observation lacks
    expectation attributes
  - `f649818` fix(app): make workspace invalidation a real retry point for
    quick open
  - `d5d0dd3` docs: correct lane-e contracts and threat model after
    independent verification
- Prior verdict under re-verification: needs-fix,
  `docs/specs/conformance/verify-lane-e.md` on branch `verify-lane-e`
  (`55de395`)
- Environment: macOS 26 (arm64, case-insensitive APFS), Go 1.27.4
- Method: re-read every fix commit, re-ran the implementer's regression
  tests, re-ran the original verifier's trap probe against the fixed tree,
  wrote independent probe tests for every fix claim, reproduced the
  claimed mutation verification, full-suite regression plus race.

Re-verifier-added probe files (new tests only — no Lane E implementation
code modified; the one mutation was applied, measured, and reverted
uncommitted):

- `internal/fileobs/zz_reverify_probe_test.go` — the attack reproduced
  from the report's description with the pre-fix `Check` reconstructed
  from git history as a control; deliberate-weak-expectation contract
  probes; absence semantics; a concurrent-rename Frankenstein probe with
  a discriminating stat-then-open control.
- `internal/app/zz_reverify_quick_test.go` — two-invalidation semantics,
  mid-scan invalidation dedup, hot-loop non-resurrection across frames,
  command-query invalidation scoping.

## Verdict: merge-ready

Every defect the original verification charged is fixed or honestly
re-scoped, each fix is pinned by a test that demonstrably fails without
it, and no fix introduced a regression or a forbidden symmetry. The two
standing deviations (case-variant workspace-binary evasion, unredacted
server stderr) are integration-phase items by design — both lanes agree
on mechanism and ownership — and the documents now say so at the claim,
with reproduction steps and integration checklist entries. Nothing in
Lane E's own scope remains open.

## D2 — fileobs fail-closed Check: confirmed-fixed

1. **The attack is loudly rejected.** `TestCheckRefusesToDropExpectationAttributes`
   passes (3 consecutive runs). Independently: I reconstructed the pre-fix
   intersection-only `Check` verbatim from `e593858` into my probe as a
   control. Against the verifier's attack (same-length `hello`→`HELLO`
   replacement, mtime restored), the control passes the condition silently
   — proving the trap was real — while the fixed `Check` returns
   `ErrIncomparableIdentity`, and the honestly wired comparison
   (`ObserveContent` at both ends) returns `ErrConditionMismatch`.
2. **The original verifier's own trap probe convicts the fix.** Running
   `zz_verify_seam_test.go` extracted unmodified from `55de395` against
   this tree fails exactly where it must: `Check = identity tokens cannot
   be compared: the expectation carries digest, which the current
   observation does not measure; the trap does not exist`. The failure is
   at the documented seam, with the missing attribute named in the error.
3. **Asymmetry is correct, not overcorrected.** My probes pin both halves:
   a weak (size+mtime) expectation against a digest-carrying observation
   evaluates nil — extras are ignored — and the deliberately weak
   condition still passes the same attack *silently and correctly*, which
   is the documented contract limit: strictness cannot detect an
   expectation that was never strong, and a future "symmetric strictness"
   patch would fail `TestReverifyWeakExpectationIsLegitimate`. The
   legitimate weak use is not killed. Absence semantics unaffected:
   both-absent nil, absent-vs-present is mismatch, cross-source still
   incomparable.
4. **Frankenstein protection is real and empirically meaningful.** The
   structural argument checks out: `ObserveContent` uses one `os.Open`
   file object for both `f.Stat()` and `DigestSHA256(f)`; POSIX binds an
   open file description to its inode, so path replacement cannot split
   the attributes. Empirically, under continuous atomic-rename swapping
   of two content/mtime generations: **0 inconsistent observations in
   ~24k**, while the stat-then-open control in the same process caught
   **150–173 Frankenstein observations** per 2-second run (3 consecutive
   runs, also clean under `-race`). The probe discriminates and the
   constructor survives it. (Note: a same-inode in-place writer can still
   tear a reading digest — inherent to content observation, correctly
   disclaimed in the constructor's comment.)
5. **§B instructions are executable.** They name the exact constructor at
   both ends (`ObserveContent` at load/post-save and at commit), name
   Lane C's real closure shape (`atomicfile.CommitIf` — verified present
   in `fix-lane-c-write-contracts` at `120be60`, cond after fsync+close
   before rename), state what `ErrIncomparableIdentity` reaching the save
   path means, and warn against weakening the expectation to silence it.
   Checked against Lane C's actual API: accurate.

## D3 — quick open invalidation retry: confirmed-fixed

1. `TestQuickOpenInvalidationRetriesScan` passes (3 consecutive runs):
   empty batch retries nothing, one invalidation retries exactly once,
   healed scanner's files arrive with the panel never closed.
2. **Exactly-once semantics, independently measured.** My probes answer
   what the implementer's test does not: two sequential invalidations
   produce exactly two scans (not deduplicated away, not multiplied); a
   burst of invalidations landing mid-scan stacks nothing (the in-flight
   `q.listing` dedups them); command-mode queries keep their results and
   trigger no scan at all.
3. **The hot loop cannot re-arm.** After an invalidation-triggered retry
   fails sticky again, 20 frames produce zero additional scans
   (independently measured). The mechanism is event-driven: nothing in
   the frame path clears `q.err`; only `applyEvents` with a non-empty
   batch and `openQuickly` do. The respin's ranking invalidation
   (`q.ranked = "\x00"` in `watch.go`, scoped to file-consuming queries)
   re-enters `ensureFiles` exactly once per invalidation because the
   failed retry's completion re-keys the ranking to the query.
4. **Mutation verification reproduced.** The implementer claims the test
   times out failing without the ranking invalidation. I reverted exactly
   the added assignment `q.ranked = "\x00"` in `internal/app/watch.go`
   (mutant builds clean) and ran `TestQuickOpenInvalidationRetriesScan`:
   **FAIL after 60.2s — `condition never held` at the retry assertion**,
   matching the claim precisely. The mutation was then reverted; the tree
   is byte-identical to `d5d0dd3` plus the probe files.

## Threat model corrections: confirmed-fixed

The three corrections are semantically consistent end-to-end, with no
residual contradiction found by full-text review:

- **Credential boundary**: server stderr is now untrusted content that
  may carry configuration secrets, entering the log redacted; the
  verbatim pass-through is a recorded defect until integration, stated in
  the same paragraph. In-scope item 3 matches it verbatim in substance
  (neither through See's own speech nor through server stderr). Out-of-scope
  item 5 is narrowed to the server's own artifacts — files, network
  peers, its own logs — and explicitly re-includes bytes that cross into
  See's surfaces. No leftover instance of the old "outside See's
  boundary / by its own choice" position anywhere in the document
  (grep + full read).
- The workspace-binary invariant carries its provisional status **at the
  claim itself** in the boundary section, in the evidence table, and in
  the conformance matrix (`REQ-SEC-050: partial`), consistently worded
  everywhere.
- The evidence table distinguishes "Secrets out of See's own logging"
  (verified today) from "Secrets out of server stderr" (pending wiring) —
  the granularity the contradiction originally hid.

§F/§G integration checklist completeness — all three required items
present and precise:

1. **Exactly one redaction layer** — §F item 1: Lane A's streaming
   redactor wired once at the tail before the log write, no stacking,
   no bypass, with the verification method specified (environment-echoing
   server; `TestConfiguredServerSecretsStayOutOfTheLog` must pass
   unmodified). §F item 2 correctly subordinates redaction to not-logging.
2. **diskSpelling coverage of every executable resolver** — §G item 2
   names `languages.FindConfigured` and `search.FindRipgrep` explicitly.
   I confirmed both lexical shapes in this tree: `configured.go:105-113`
   (`EvalSymlinks` + `workspace.Within`) and `search.go` `FindRipgrep`'s
   purely lexical `filepath.Rel` exclusion — the latter never even
   resolves symlinks, so the gap is real and the checklist item is
   load-bearing, not ceremonial.
3. **One implementation survives the merge** — §G item 4: the
   Lane A/Lane B `diskSpelling` duplication must converge in the same
   integration wave.

## D4/D5 — documentation honesty: confirmed-fixed

- **D4**: the `FuzzFiles` comment now states the coverage truthfully. I
  verified the underlying numbers rather than trusting either revision:
  `Files` shards at `len(paths)/4096` workers (≥4097 paths for a second
  shard) while the harness caps at 512 — the parallel path is unreachable
  from the fuzz target. `TestLargeFileQueriesKeepOrderAndHits` uses
  20,000 paths with GOMAXPROCS 4-vs-1 comparison, so parallel/serial
  equivalence is genuinely pinned where the comment says it is. The
  fuzzing doc's target table never claimed otherwise.
- **D5**: `Stale`'s comment now documents the code's actual behavior
  (incomparable counts as stale) with the fail-safe reasoning. The
  previous wording contradicted `Check(o, later) != nil`; the correction
  chose the fail-safe reading and defends it — correct call for a cache
  invalidation primitive.

## Regression

| Check | Result |
| --- | --- |
| `go build ./...` | clean |
| `CGO_ENABLED=0 go test -count=1 ./...` | **PASS, 26/26 packages** (run twice: once pre-mutation on the pure tree, once final with probes included) |
| `go test -race ./internal/fileobs/ ./internal/search/` | clean |
| implementer fix tests ×3 runs | all pass |
| re-verifier probes ×3 runs | all pass (incl. `-race` on the Frankenstein probe) |
| original verifier trap probe vs this tree | fails exactly at the fixed seam — the trap is gone |

## Residual observations (not blocking; recorded for integration)

1. **Scan freshness during invalidation** (pre-existing, not the respin's
   scope): an invalidation landing mid-scan does not queue a rescan, so
   the in-flight scan's result can be one event stale with no further
   retry until the next event. This is the invalidation/scan coherence
   design as it exists on the base commit, affects the healthy path
   equally, and is unchanged by the respin — worth a decision at
   integration, not a defect introduced here.
2. **`ObserveContent` on a directory errors** (os.Open succeeds, the read
   fails) where `ObservePath` stats happily. The contract documents
   observations of files; the constructors do not say "file" in so many
   words. No consumer is wired yet, so this is a contract-sharpening note
   for integration, not a bug.
3. The threat model's two integration-pending paragraphs (stderr
   redaction, disk-spelling denial) describe target-state boundary in
   normative voice with the gap marked in the same paragraph. The marking
   is consistent and unambiguous, but readers of the final integrated
   document should expect these paragraphs to flip from "recorded defect"
   to verified claims — the integration phase must update them, and §G
   does not yet say so explicitly. Trivial.
