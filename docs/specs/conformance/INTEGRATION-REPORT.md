# Integration conformance report — five spec lanes, one tree

- Integration revision: branch `spec-integration` (see commit footer), based on the lane chain `spec-lane-b-paths` ← `spec-lane-a-tools` ← `spec-lane-e-round2`, with `spec-lane-c-writes` and `spec-lane-d-io` merged in
- Environment: macOS darwin/arm64 (case-insensitive APFS), Go 1.27.x, CGO_ENABLED=0
- Method: per SPEC-000 — requirement ledger, behavioral evidence at real entry points, mutation evidence where a test's discrimination matters

This report is the integration phase's own artifact: it does not re-verify the
lanes (each was independently verified and re-verified before integration), it
records what the integration changed, what it proved, and what remains open.
Lane-level requirement matrices: [lane-a](lane-a-conformance.md) ·
[lane-b](lane-b-conformance.md) · [lane-c](lane-c-conformance.md) ·
[lane-d](lane-d-conformance.md) · [lane-e](lane-e-conformance.md).
Verification chain: `verify-lane-{a..e}` and `reverify-lane-{a..e}` branches;
the integration contract is [lane-e-integration-notes.md](lane-e-integration-notes.md).

## Requirement ledger (per lane, evaluated-path counts)

| Lane | Specs | MUST coverage per its matrix | Lane verdict |
| --- | --- | --- | --- |
| A — negotiated LSP operations, external processes | SPEC-001/002/004/005 | 30 implemented / 9 partial of 39 (evaluated paths) | merge-ready (reverify-lane-a) |
| B — resource identity and path boundaries | SPEC-006 | 8/9 MUST implemented; REQ-PATH-080 partial (residual D2: localhistory keys physical canonicalPath, low impact) | merge-ready (reverify-lane-b) |
| C — transactional writes, versioned configuration | SPEC-003/007 | 18 implemented / 2 partial / 1 documented divergent (REQ-WR-080, SHOULD) of 21 | merge-ready (reverify-lane-c) |
| D — streaming and range IO | SPEC-008 | 9/9 implemented on evaluated paths; REQ-IO-080 scenario 6 cross-lane partial noted | merge-ready (reverify-lane-d) |
| E — round-2 mechanisms (checksum, fuzz, threat model, scan) | SPEC-009 | D 7/7 (install surface), E 6/6, G 4/6 + 2 narrowed partials; B contract package, C interface only — wired here | merge-ready (reverify-lane-e) |

The integration changes exactly three cells' meaning:

- **REQ-COND-010/020/040/050/060/070/080** (lane C+E, was "partial — contract
  written"): now wired end to end, see §1 below. REQ-WR-070's residual is
  honestly narrowed, not eliminated: the window is the rename boundary now,
  not the entire staging write.
- **REQ-SEC-050** (lane E, was "partial — provisional"): the workspace-binary
  invariant now holds in the disk's own spelling across every executable
  resolver — provisional lifted, see §2.
- **REQ-SEC-030** (server-stderr half) / **REQ-SEC-040** (LSP parser half):
  both halves verified in the integrated tree, see §3 and §4.
- **REQ-CHK-030** (local-history half, was "missing — Lane C wiring"): snapshot
  digests wired, see §5.

## §1 Conditional save (REQ-COND-050/060/070) — the P0 wiring

`saveDoc` publishes through `atomicfile.CommitIf`: the condition "the target
still holds the content this buffer was read from" evaluates after the staging
file is durable and closed, immediately before the rename. Design decisions
the contract does not spell out, made and recorded here:

- **The expectation is digest-only and built from the bytes read, never
  re-observed.** `fileobs.ExpectContent` (new constructor) digests the exact
  bytes the buffer was loaded from — re-observing the file at load/save time
  would race the file instead of measuring it, and carrying size+mtime would
  regress the identical-content-rewrite case to false conflicts. Content
  strength matches and sharpens the pre-integration byte-compare semantics.
- **The re-observation is `ObserveContent`, per the contract.** Both
  mutations were verified: dropping the condition fails
  `TestWriteFileIfUnchangedRefusesMismatch`; wiring `ObservePath` instead
  fails the suite at the `ErrIncomparableIdentity` seam — the fail-closed
  `Check` does its job.
- **`ErrConditionMismatch` surfaces as the existing conflict UI**
  (`d.conflict` + the review note); `ErrIncomparableIdentity` surfaces as a
  save failure, never a conflict — it means the wiring deviated.
- **Overwrite bypasses the condition by design**: the user reviewed the
  conflict. A fresh external write racing the overwrite itself remains subject
  to the rename-window limitation.
- **REQ-COND-070 honesty**: the conflict semantics the user gets are
  best-effort detection whose race window is the rename syscall — documented
  in `writeFileIfUnchanged`'s comment and in atomicfile's package comment.
  No claim of atomic compare-and-publish exists anywhere.
- **REQ-COND-080 batch independence**: workspace replace keeps per-file
  outcomes (`internal/search/replace.go` unchanged, its own hash-checked
  transaction); the save path is per-document by construction.

Behavioral evidence: `TestWriteFileIfUnchanged{RefusesMismatch,PublishesWhen
ContentMatches,RejectsWeakReobservation,OverwriteBypassesCondition}` (direct
boundary), `TestSaveRefusesAtPublishWhenFileChangesMidWrite`,
`TestSaveConditionIsContentStrength` (identical rewrite passes, same-length
same-mtime replacement conflicts — both halves a size+mtime condition
inverts), `TestNormalSavePublishesAndReanchors`,
`TestRecoveredEditsRefuseToOverwriteExternalChanges` (pre-existing, passes
unmodified).

Companion fix (Lane C reverify R-2): `CommitIf` evaluates cond after the
session lock rather than under it — the lock bought nothing there (finished
is already set) and made a re-entrant cond a permanent self-deadlock.
`TestCommitIfReentrantCondDoesNotDeadlock` would hang on the pre-fix shape.
Placed in the integration branch because the wart matters exactly once this
wiring lands.

## §2 diskSpelling convergence (integration notes §G items 2+4)

One implementation survives: `workspace.DiskSpelling` (full Unicode case
folding, Lane B's). Lane A's `internal/proc/diskspelling.go` (simple lower —
correctly marked for deletion) is gone; `proc.ValidateExecutable` and its
tests use the workspace primitive.

Coverage of every executable resolver, not just the one Lane A owned:

- `languages.FindConfigured` and all built-in finders route through
  `validateCandidate` → `proc.ValidateExecutable` (already, verified).
- **`search.FindRipgrep` was the gap**: its workspace exclusion was a lexical
  `filepath.Rel` comparison that neither resolved symlinks nor normalized
  spelling. It now applies the same `proc.ValidateExecutable`, keeping the
  finder semantics: PATH-discovered copies skip silently, explicit candidates
  (`SEE_RG`, managed installs) report the typed denial. Mutation evidence:
  restoring the lexical comparison fails `TestFindRipgrepDeniesWorkspaceVariants`,
  which covers exact, case-variant and symlink spellings plus the
  silent-PATH-skip behavior.

The threat model's provisional paragraph is flipped to a verified claim at
`SECURITY-THREAT-MODEL.md`.

## §3 stderr redaction — exactly one layer (§G item 3)

No wiring needed: Lane A's shape won the merge. Server stderr pipes through
exactly one `proc.Tail` (redacting as bytes arrive, before the capture-window
trim) into exit diagnostics; Lane E's verbatim streaming `tail` type no longer
exists in `internal/lsp/client.go`. No second redactor is stacked anywhere.
Verified: `TestConfiguredServerSecretsStayOutOfTheLog` passes unmodified in
the integrated tree, `TestStartCrashRedactsOversizeStderrSecret` covers
oversize secrets byte by byte.

## §4 LSP frame parser fuzz (§E wiring order, item 1)

`FuzzReadMessage` in `internal/lsp/fuzz_test.go`, per the contract in
[lane-e-fuzzing.md](lane-e-fuzzing.md): near-valid shaped inputs (validly
framed chunks, amplified declared lengths clamped to 1 MiB, corrupted header
structure), input ≤ 64 KiB, behavioral oracles (no panic, forward progress —
the deadlock shape, no frame body larger than its stream, no EOF with bytes
unread), stream fed in input-derived 1–64-byte chunks. Evidence: 60s run,
**6.09M execs, zero failures**; no crash inputs to commit.

The three lower-priority unwired targets (path normalization, hex paging,
edit sequences) remain documented contracts in lane-e-fuzzing.md — recorded
here as future work, not silently dropped.

## §5 local history digest (§D)

`Snapshot` carries a full hex SHA-256 `digest` measured when written.
`Store.List` verifies every digest-bearing snapshot at read time; a mismatch
(or malformed digest — fail-closed) marks the snapshot corrupt: identity
preserved in the picker with a localized corrupt tag, text never reaches a
diff, restore structurally impossible. Legacy digestless records load
unverified — the honest migration state. Evidence:
`TestLocalHistoryCorruptSnapshotNeverReachesBuffer` (buffer byte-identical
after the restore attempt, intact sibling still restores),
`TestLocalHistoryLegacySnapshotWithoutDigestRestores`, store-level round-trip
and malformed-digest tests.

## §6 Scan freshness (Lane E reverify residual 1) — fixed

An invalidation landing mid-scan no longer lets the scan commit its stale
result: scans carry a generation, invalidated in-flight completions are
discarded, and the ranking is invalidated so a consumer re-enters the scan.
`TestQuickOpenInvalidationDuringScanRescans` pins it; mutation evidence in
its comment. (The alternative — documenting the staleness — was rejected: the
fix is small and the failure mode user-visible.)

## §7 Merge adjudications, for the record

| Where | What met | Disposition |
| --- | --- | --- |
| `internal/search/search.go` | Lane B's ValidRel filtering, Lane E's preview/walk work, integration's FindRipgrep rework | merged cleanly — disjoint regions; all three verified present in the merged file |
| `internal/app/codeaction.go` | Lane B's DiskSpelling canonicalization of workspace-edit URIs, Lane A's preflight+version gate | both kept: canonicalization moved to path construction so the version-check lookup and the apply loop share one canonical key |
| `locales/*` | lane A's degraded/crash strings vs lane C's config-load notice | unioned; catalog regenerated with `go tool mygo generate .` |
| Lane A base | forked before SPEC-009 existed | healed through the B-merge chain |
| Lane D base | same | merged base forward, one commit |

## Verification evidence at the integration revision

See the layer-by-layer log in the PR description. Summary:

- `go build ./...` clean at every layer.
- `CGO_ENABLED=0 go test -count=1 ./...` — full suite at the A layer (26
  packages), the E layer (27), and the integration tip: see report footer.
- Mutation evidence collected inline above for every new mechanism.

## Known pre-existing issues (not introduced here, not fixed here)

- `TestHexViewReanchorsToTheChangedVersion` flake: passes in most runs
  and in both full-suite runs at the integration tip, but fails
  intermittently in isolation (~half of sampled runs) with the re-pinned
  view showing only page 0 where the test expects the jumped page — a
  re-pin/re-request timing race. Reproduced identically on lane-d's own
  branch (`108bc7c`), so it predates integration; lane-d's verification
  sampled the passing side. Root-cause and fix belong to a lane-D
  follow-up; it is recorded here rather than silently re-run until green.
- `TestNewFileCacheFailureKeepsWelcome` flake: test-isolation race —
  the test deletes the shared cache dir while a debounced localhistory
  capture may still write it. Probabilistic; confirmed pre-existing on both
  lane-D base and fix tip by reverify-lane-d. Needs a private cache root or
  serialization; a separate fix.
- Recovery-record crash window on rename (reverify-lane-b "对抗一击"):
  `renamed()` does remove-then-recache; a crash in between loses the unsaved
  edit's recovery record. Pre-existing contract gap for all renames.
- Workspace-symlink dual identity: stale views unmarked (lane-B known gap,
  VS Code alike); case-only-rename + old-gone-tab save can overwrite
  invisibly (lane-B known gap F4).
- Windows-only behaviors unverified throughout (suite runs on Darwin).

## Residual integration debt (explicit, none blocking)

- **`main` divergence**: the chain is based on `specs-opendal-dispatch`
  (f3d214c), which is 2 ahead / 11 behind `main` at integration time.
  Merging the chain into main requires forward-fitting lanes that overlap
  main's surface (probe: lane A 2 files, B 3, C 3, D 0, E 1 conflicted).
  Decision deferred to the chain owner — see the INTEGRATION-REPORT note in
  the integration PR description.
- REQ-IO-080 scenario 6: task/terminal output truncation visibility is a
  Lane A surface, unchanged; cross-lane partial stands.
- Three fuzz contracts unwired by design (path normalization, hex paging,
  edit sequences) — lane-e-fuzzing.md.
- Lane B's D2 (localhistory keyed by physical canonicalPath) — untouched,
  owner decision.
