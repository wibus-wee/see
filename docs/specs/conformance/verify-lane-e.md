# Independent verification of Lane E — SPEC-009 round 2

- Verifier: independent verification engineer (adversarial mandate: implementer
  claims count for nothing unless reproduced)
- Target: branch `spec-lane-e-round2`, tip `e593858`
- Specification: `docs/specs/09-round2-advanced-mechanisms.md` at `f3d214c`
- Environment: macOS 26 (arm64, case-insensitive APFS), Go 1.27.4
- Method: re-derived every claim from code, re-ran implementer evidence, ran
  additional adversarial fuzzing (about 12 core-minutes beyond the implementer's
  runs), wrote independent probe tests where implementer coverage had blind
  spots, and cross-checked Lane A (`spec-lane-a-tools` at `e86dda2`) and Lane C
  (`fix-lane-c-write-contracts` at `b05ec1d`) for seam fit.

Verifier-added test files (new tests only — no Lane E implementation code was
modified):

- `internal/search/zz_verify_boundary_test.go` — deterministic boundary sweep
  of `preview`/`unflip` (multibyte characters at window edges, out-of-contract
  coordinates, CRLF seam), plus a stronger oracle than the fuzz one: in-contract
  matches must be fully covered by the highlight span.
- `internal/app/zz_verify_recovery_test.go` — real ripgrep against a real
  permission-broken directory; the watcher-invalidation retry path for both
  consumers (quick open panel, tree filter).
- `internal/app/zz_verify_casetest_test.go` — case-variant workspace-binary
  resolution probe.
- `internal/fileobs/zz_verify_seam_test.go` — the Check intersection-semantics
  downgrade seam.

## Verdict: needs-fix

Lane E's code-level work is solid: the fuzz fixes are structural, not patches;
the hot-loop fix is correct and its recovery path verified end-to-end with real
ripgrep; the checksum chain does what the tests claim; no new crashes found by
independent fuzzing. What blocks merge-ready is on Lane E's own deliverable
surface — the documents and contracts:

1. **D2 (high, contract bug)**: the integration contract for the save path
   (lane-e-integration-notes §B step 2) as literally written produces a save
   condition strictly weaker than today's behavior — exactly the regression its
   own step 4 forbids. Demonstrated empirically.
2. **D1 (medium)**: the threat model's first in-scope invariant — workspace
   binaries never execute — does not hold on default macOS volumes: a
   case-variant spelling of the workspace path evades the lexical `Within`
   rejection (same defect Lane A independently found and fixed in its own
   lane). Either the threat model overclaims, or the invariant needs the fix.
3. **C1 (cross-lane contradiction, needs resolution at integration)**: server
   stderr. Lane E streams it verbatim into the log as "the server's own
   speech"; Lane A redacts it against the process environment. These are
   incompatible positions on the same byte stream.
4. Minor: quick-open panel does not rescan on invalidation (commit message
   overclaims), fuzz doc overclaims parallel-shard coverage.

## Fuzzing verdict (Section E) — the central adversarial question

**No new crashes found.** Evidence:

- All 9 targets re-run at 90s each (implementer ran 20s/15s): all PASS.
  FuzzPreview executed 3.26M inputs, FuzzReplaceMatches 5.28M, FuzzEvaluate
  1.42M. Coverage-guided exploration saturated early on the two slicing targets
  (FuzzPreview found only 1 new interesting input in 90s) — the input space the
  harness can reach is small and well-covered.
- A second round (120s/90s) on the three most arithmetic-sensitive targets:
  all PASS, zero new corpus entries.
- Because coverage-guided mutation is weak at precise arithmetic relationships,
  I wrote a deterministic boundary sweep instead: multibyte characters (2- and
  3-byte) at every position of lines sized around the 200-byte window,
  start/end at every in-contract and ±1..5 out-of-contract offset, match
  lengths up to 300 (> previewWidth), CRLF-trimmed lines with end coordinates
  inside the trimmed `\r`. ~500k cases, zero invariant violations — including
  the stronger oracle (in-contract matches fully covered by the highlight).
- `unflip` swept exhaustively over indexes -2..flipped+2 for 8 path shapes
  including unicode and degenerate paths: zero out-of-bounds hits.

**The regression seeds are genuine.** I reconstructed the pre-fix `preview`
and `unflip` from `91ecbf1^` and ran the three committed seeds against it:

- `5eff94d7a6f80295` (`"\xc3"`, 78, 89): **panics the pre-fix code** —
  `index out of range [38] with length 1`. Real crash, correctly pinned.
- `ddce8ccd48a1a5a1` (`"0"`, 0, -32): pre-fix emits `PreEnd=-32 < PreStart=0`,
  violating the coordinate invariant. Real defect, correctly pinned.
- `c2c667dfd3b26e9f` (201 bytes `\x87` + `'0'`×200, 0, 0): does **not** fail
  the pre-fix code as committed in git history — it pins the "rune alignment
  moves window start past match" bug of the intermediate rewrite (reproducible
  by removing the `from < start` walk guard: it then panics). The implementer's
  account is consistent with this; the seed pins the window-invariant class of
  bug, which the final code prevents structurally. All three seeds execute as
  regression tests in ordinary `go test` (verified).

**Window-invariant completeness review** (the "structural or patch?" question):
the fixed windowing holds the invariant structurally: (1) `from` is computed,
then `to = from + previewWidth`, then `to = max(to, end)` — so `to ≥ end`
before alignment; (2) the `to`-walk only ever *increases* `to`, preserving
`to ≥ end`; (3) the `from`-walk is bounded by `from < start`, so `from` never
crosses the match; (4) `start, end` are clamped into `[0, len(text)]` before
any of this. The one residual is cosmetic: in-contract impossible input
(invalid UTF-8 with match end mid-character) can clip the match end by up to 2
bytes. REQ-E assessment: **the fix is structural, not a patch.**

REQ-level verdicts:

| Req | Verdict | Evidence |
| --- | --- | --- |
| REQ-FUZZ-010 | confirmed | harnesses decode near-valid shapes; matches point at real lines, corrupting some hashes |
| REQ-FUZZ-020 | confirmed | budgets documented and enforced pre-work |
| REQ-FUZZ-030 | confirmed | worst-case composition bounded; no input-controlled iteration independent of clamped size |
| REQ-FUZZ-040 | confirmed | seeds verified as genuine pins against pre-fix code (see above); pinned in CI by `go test` |
| REQ-FUZZ-050 | confirmed | no network/process use in any target |
| REQ-FUZZ-060 | confirmed | oracles behavioral; one documentation overclaim, see D4 |

## Section G — scan hot-loop and stream semantics

**The hot-loop defect and fix are real and correct.** I reproduced the failure
mode upstream: real ripgrep 15.0 against a directory with a `chmod 000`
subdirectory prints valid entries to stdout *and* exits 2 — exactly the
partial-failure shape that made the pre-fix code loop a process per frame.
Implementer's `TestQuickOpenRetriesAfterAScanError` verified: 15 frames add
zero scans, reopening retries exactly once, a healed scanner yields files
(flaky-check: 3 consecutive runs pass). My own additions:

- `TestVerifyRealRipgrepScanFailureAndRecovery` — real rg, real permission
  failure: `ListFiles` errors with stderr preserved, returns nil files, no
  truncation; after `chmod 755` healing, the next scan returns both files. PASS.
- `TestVerifyWatcherInvalidationRetriesScan` — **partial failure of the
  implementer's claim** (D3 below).
- `TestVerifyTreeFilterRetriesScanOnInvalidation` — PASS: for the tree filter,
  invalidation *does* rescan on the next frame (`syncFilter` → `ensureFiles`),
  and healing reaches the filter. The documented retry point works for this
  consumer.

**D3 (minor, behavior/doc gap): invalidation is not a retry point for the
quick open panel.** The commit message says "the two deliberate retry points
are opening the panel and workspace invalidations". For the tree filter this is
true. For the quick open panel it is not: invalidation clears `q.err` and
`q.files`, but `rankQuick` returns early because `q.ranked == q.query` — the
query hasn't changed — so nothing ever calls `ensureFiles` again, and the
panel sits on the error text while the filesystem changes underneath it, until
the user retypes or reopens. Measured: 10 frames after invalidation, zero
scans (scanner invocation count unchanged), while the same invalidation drives
an immediate rescan through the tree filter path. The panel's actual retry
points are: reopen, retype, or the Retry button **of the tree filter** (not
present in quick open). Severity minor — the natural retry (close/reopen)
works, and the error is never mistaken for content — but the commit message
and the `ensureFiles` comment overclaim the mechanism, and a user watching the
panel while fixing permissions sees a stale error. Fix direction: invalidate
`q.ranked` alongside `q.files` in the invalidation sites, or document the
narrower contract.

| Req | Verdict | Evidence |
| --- | --- | --- |
| REQ-LIST-010 | confirmed (as narrowed) | no directory-mode listing exists; narrowing documented on `ListFiles` |
| REQ-LIST-020 | confirmed | implementer tests + my real-ripgrep probe; walk fallback verified in code: `nil, false, err` on error |
| REQ-LIST-030 | confirmed | cap enforced; cancellation test passes (implementer's); memory bounded by cap + 64 KiB scanner line |
| REQ-LIST-040 | confirmed | no versioned-scan capability exists to degrade |
| REQ-LIST-050 | confirmed (as narrowed) | backend/client filter split documented honestly |
| REQ-LIST-060 | confirmed | contract documented; verified no caller deduplicates (grep) |

Side observation: a >64 KiB single line of scanner output (e.g. a pathological
filename) errors the whole scan rather than being skipped — consistent with
fail-closed semantics; undocumented but defensible. Not a defect.

## Section D — checksum chain

The implementer's tests pass and are honest: mismatch before any write,
parent directory empty after failure, existing installation byte-identical
after a corrupted-mirror episode, second install refused. My additional checks:

- localhistory carries **no** content integrity: confirmed by reading
  `internal/localhistory/store.go` — `snapshotID` mixes path+time+text into an
  *identifier*; nothing recomputes anything at restore. The integration-notes
  claim is accurate.
- POSIX fail-closed check for the install TOCTOU: `os.Rename` over an existing
  directory fails on this platform even when the destination is **empty**
  (verified empirically), so the Lstat-then-rename race cannot overwrite a
  racing installation on macOS/Linux. Residual risk window is benign.
- `verifyChecksum` is fail-closed on empty hash-less checksum files (unit
  tests cover), uses full-object comparison before extraction, and the staged
  rename only happens after validation. gopls delegation to the Go module
  checksum database is real (`go install` with pinned version) and documented
  as delegation, not verification — honest.
- REQ-CHK-050 range-read non-application: confirmed by grep — no checksum
  machinery in hex viewer / paging paths.

| Req | Verdict | Note |
| --- | --- | --- |
| REQ-CHK-010/020/040/050/060/070 | confirmed | install surface, per implementer evidence re-run + code reading |
| REQ-CHK-030 | confirmed for install; confirmed-missing for local history | Lane C wiring item with a complete contract — accurate |

## Section F — threat model

Spot-checked claims:

- **Trust gating**: verified in `internal/app/tasks.go` (task invocation raises
  the trust prompt), `internal/app/app.go:1066-1069` (documents handed to
  servers only when trusted). Confirmed.
- **Log ring 1000×8192**: confirmed in `internal/logs`.
- **Playground budgets**: confirmed — `MaxPatternBytes 4096`,
  `MaxSampleBytes 128 KiB`, `MaxMatches 100`, enforced before compile.
- **`TestConfiguredServerSecretsStayOutOfTheLog`**: passes (3× stable). I
  audited all log call sites for argv/env leakage: `lsp/client.go:107` logs
  only the binary path element of the command (arguments excluded);
  `serverStateLog` formats the state error, which carries no env/argv; env
  never formatted anywhere. **The claim "secret-shaped inputs are never
  logged" is true of See's own speech today.** But see C1: the *server's*
  speech is a hole in in-scope item 3 as written.
- **D1 (medium): workspace-binary denial is evadable by case-variant paths on
  case-insensitive volumes.** The threat model claims language server binaries
  are "never executed from inside the workspace … comparing symlink-resolved
  paths". Verified empirically on this machine (default case-insensitive
  APFS): `EvalSymlinks` resolves `/var`→`/private/var` but does **not**
  normalize case, so `workspace.Within` — purely lexical — fails to recognize
  `workSpaceMixedCase/tools/evil-lsp` as inside `WorkSpaceMixedCase`.
  `TestVerifyCaseVariantWorkspaceBinaryResolution` demonstrates
  `languages.FindConfigured` resolving a case-variant in-workspace binary that
  the exact-case spelling rejects; `StartConfigured` passes the resolved path
  straight to `lsp.Start`, so the denial is defeated end-to-end. Preconditions:
  trusted workspace + a configured path whose case differs from both the
  opened root's spelling and the disk's — narrow but plausible (users copy
  paths; macOS volume spelling vs. user memory diverge casually). This is the
  same defect Lane A independently found in `proc.ValidateExecutable` and
  fixed with `diskSpelling` probing (`spec-lane-a-tools` commit `578004e`).
  Lane E's threat model verified only the exact-case spelling, so its first
  in-scope invariant does not hold on the platform it was verified on. Note
  `search.FindRipgrep`'s workspace exclusion has the same lexical shape.
- **`TestConfiguredServerInsideWorkspaceNeverStarts`**: passes, but only tests
  the exact-case spelling — it cannot catch D1.

| Req | Verdict | Note |
| --- | --- | --- |
| REQ-SEC-010 | confirmed | document structure complete |
| REQ-SEC-020 | confirmed | no credentials held; per-workspace trust keying verified |
| REQ-SEC-030 | confirmed for See's own speech; contested for server speech | see C1 |
| REQ-SEC-040 | confirmed for owned surface | fuzz evidence above |
| REQ-SEC-050 | **partially refuted** | D1: invariant false on case-insensitive volumes; local history 0700/0600 verified in `store.go`; install staging verified |
| REQ-SEC-060 | confirmed | bounds enumeration accurate |
| REQ-SEC-070 | confirmed as partial-process | honest self-assessment |

## Section B — fileobs

The package itself is high quality: immutability verified by test re-run,
presence bits correct, `Tri` honest, cross-source comparison refused,
absence-vs-empty distinguished.

**D2 (high, contract bug): the save-path wiring contract as written produces
the regression its own step 4 forbids.** Integration notes §B step 2 prescribes
evaluating `fileobs.Check(expected, fileobs.ObservePath(SourceRead, path))` at
commit. But `ObservePath` carries only size+mtime — never the digest — while
`Check` compares only the **intersection** of attributes. Demonstrated
empirically (`TestVerifyCheckSilentlyDropsUnsharedDigest`): content replaced
with same-length bytes and mtime restored passes the check, because the
expectation's digest is silently dropped from the comparison. Today's `saveDoc`
compares full content — strictly stronger. A Lane C implementer following the
contract literally would regress save-conflict detection from content
comparison to mtime comparison, and nothing in the type system or in `Check`
would tell them. Meanwhile step 4 of the same section explicitly warns "must
use the digest attribute, not regress to mtime" — the contract is internally
contradictory, and the contradiction is resolved in the dangerous direction by
the code shape it shows. Fix direction: either change step 2 to re-digest at
commit (`ObservePath` + `WithDigest(DigestFileSHA256)`), or better, make
`Check` strict — an attribute present in the expectation but absent in the
current observation is `ErrIncomparableIdentity`, never a silent downgrade
(and document why intersection semantics were chosen if kept). The
intersection semantics are documented on `Check`'s comment, so this is a
contract bug rather than an implementation bug, but it is exactly the kind of
bug a contract exists to prevent.

Also: `Stale`'s doc says incomparable is not staleness, but the code returns
`Check(...) != nil` which is true for incomparable pairs — unreachable through
public constructors today (every observation carries size+mtime), noted as a
latent doc/code disagreement.

| Req | Verdict |
| --- | --- |
| REQ-META-010/020/030/040/060 | confirmed (package level) |
| REQ-META-050 | confirmed-missing (deliberate, documented) |

## Section C — CommitIf seam fit

Lane C's `fix-lane-c-write-contracts` (`b05ec1d`) now has `internal/atomicfile`
with `CommitIf(cond func() error)` and `CommitIfAbsent()`:

- **Timing**: `cond` runs after fsync+close of staging, immediately before
  rename — matches Lane E's contract point 1 (evaluate at commit, not at
  begin). Lane C's own documentation of the POSIX rename-window race matches
  Lane E's REQ-COND-070 honesty requirement almost verbatim. Seam: **good**.
- **Shape mismatch (minor)**: Lane E's contract says "the atomic-write
  primitive receives the expected Observation, re-observes inside the same
  critical section"; Lane C built the more primitive `cond func() error`
  closure instead — the re-observation is the caller's problem. That is a
  reasonable API (more general), but combined with D2 it means the digest
  re-observation discipline lives in exactly one place: the caller's memory.
  `fileobs.Check` errors pass through `CommitIf` unwrapped, so error taxonomy
  (REQ-COND-030) survives the seam.
- `CommitIfAbsent` (hard-link publish) is a genuinely atomic create-if-absent
  — stronger than anything Lane E's contract anticipated; the seam has no
  consumer for it in Lane E's notes yet (worth wiring for create-file).
- Lane C does not yet import fileobs — wiring is integration-phase work on
  both sides, as both lanes document.

## Section A — assessment verification

The "don't build the framework" judgment is supported by evidence:

- `search.Options` indeed has one structured entry; verified all construction
  sites: `find.go` initial assembly + in-place mutation by UI, wholesale
  rebuild in `rerunSavedFindQuery` (the integration notes' "single construction
  site at find.go:118" is loosely worded — there are two — but both feed the
  same single validation/dispatch boundary in `search.Run`, which is the point).
- git operations do assemble overlapping invocation shapes (`git.Command`
  variadic args across restore/checkout/discard paths) — the noted genuine
  gap is real but speculative-urgency-free; the spec's own P2 guidance applies.
- REQ-API-040's uniform-fail-fast holds trivially for search because invalid
  input is nearly unrepresentable (booleans); empty query fails at the single
  boundary. Assessment stands.

## Cross-lane contradiction register

**C1 — server stderr: verbatim vs redaction.**
Lane E's `internal/lsp/client.go` `tail.Write` streams every stderr chunk
verbatim into the log (`server stderr: %s`), documented as the server's own
speech boundary: "a server that prints its environment leaks it by its own
choice, outside See's boundary." Lane A's `proc.NewTail` redacts stderr
against the process environment (secret-named variables, long token-shaped
values, URL userinfo passwords) with split-write-safe streaming redaction —
its position: "a server's stderr is untrusted content: it is redacted against
the environment it runs with."
This is not a cosmetic difference. Real LSP servers print crash diagnostics
and sometimes their environment (especially in debug or failure modes); users
put real tokens in `LSPConfig.Env` precisely so servers can use them, and the
log panel is explicitly a paste-into-issues surface per Lane E's own test
comment. Lane E's in-scope item 3 says "Secrets the user places in server
configuration must not leak into See's own logging" — the verbatim pass-through
violates that sentence whenever a misbehaving or crashing server echoes its
environment, while Lane E's boundary section declares the same behavior out
of scope. The threat model is internally inconsistent here, and the two lanes
must converge at integration: Lane A's exact-value redaction is strictly
stronger and cheap. Recommendation: adopt Lane A's redaction and amend the
threat model's server-speech paragraph.

**C2 — case-variant path handling.** Lane A fixed `proc.ValidateExecutable`
with `diskSpelling` (per-component `os.SameFile` probing); Lane E's verified
surface (`languages.FindConfigured`, `search.FindRipgrep`) has the same
lexical-comparison defect unfixed, and Lane E's threat model claims the
invariant holds. At integration the `diskSpelling` primitive (or Lane B's
`workspace.DiskSpelling`, which Lane A's NOTE says it mirrors) must be applied
to the languages/search resolvers too, or the threat model corrected.

## Defect register

| # | Severity | Kind | Status |
| --- | --- | --- | --- |
| D1 | medium | workspace-binary denial evaded by case-variant paths (case-insensitive volumes); threat-model claim false where verified | open — fix belongs at integration (Lane A's primitive exists) |
| D2 | high | integration contract §B step 2 prescribes a save-condition strictly weaker than current behavior; `Check` intersection semantics silently downgrade | open — contract must be fixed before Lane C wires it |
| D3 | minor | quick open panel does not rescan on workspace invalidation (error stays displayed while files change); commit message overclaims retry points | open — one-line fix direction noted |
| D4 | trivial | lane-e-fuzzing.md claims FuzzFiles exercises "parallel shards"; harness caps at 512 paths, parallel needs ≥4097 — unreachable. Unit test `TestLargeFileQueriesKeepOrderAndHits` does cover parallel/serial determinism, so coverage exists, the doc is just wrong | open — doc fix |
| D5 | trivial | `Stale` doc says incomparable is not staleness; code disagrees (unreachable today) | open — doc fix |

## Engineering hygiene

- `go build ./...` clean; full `go test ./internal/...` passes (all 27
  packages, ~4 min); `-race` clean on search/fileobs/regexplayground.
- Security/scan tests stable across 3 consecutive runs.
- Diff review: no unrelated carry-in; every file in the diff belongs to the
  lane's scope. Import-order churn in find.go is gofmt-level noise, one line.
- The regexplayground commit message lists evaluation/diagrams/capture spans/JS
  detection as features but the commit adds only fuzz tests — the features
  predate the lane; the message is misleading but harmless.

## What would change this verdict to merge-ready

1. Fix the integration contract §B step 2 (re-digest at commit) or make
   `Check` strict about expectation attributes absent in the current
   observation — with a test that would have caught the trap.
2. Either apply disk-spelling-aware comparison to the executable resolvers
   (coordination with Lane A/B integration), or amend the threat model to
   scope the invariant honestly ("exact-spelling paths") with the gap
   documented as a known defect.
3. Record the stderr-redaction decision: adopt Lane A's redaction (recommended)
   or consciously override it with a reasoned amendment — the two lanes cannot
   both land as-is.
4. D3/D4/D5 are small and can land in the same respin.
