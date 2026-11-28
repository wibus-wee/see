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

- `TestHexViewReanchorsToTheChangedVersion` — **fixed in the platform
  repair round** (lane-d commit "wait for the page the hex re-pin
  assertion reads"). What was recorded here as a re-pin timing race was
  the test's readiness probe firing on the first of the two concurrently
  requested pages to land: the jump straddles a page boundary, and when
  page 0 landed first the assertion read page 1 before its read
  completed. The probe now waits for the page the assertion checks —
  20/20 consecutive runs pass in isolation.
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
- Windows-only behaviors verified by CI in the platform repair round:
  the suite now runs on ubuntu/windows/macos runners. What that round
  found and fixed — merging-filesystem assumptions in the case-identity
  probes, os.SameFile's lazy re-open on Windows, volume-anchored joins,
  PATHEXT resolution, Windows ERROR_PATH_NOT_FOUND folding — is
  recorded in the lane commit messages and the lane-b conformance
  addendum.

## Residual integration debt (explicit, none blocking)

- **`main` divergence — resolved by rebase**: the chain now bases on
  `main` (8745341) directly; the lanes no longer carry the spec documents
  themselves, which land independently through the specs PR. What the
  forward-fit cost and what it adjudicated is recorded in §6 below.
- REQ-IO-080 scenario 6: task/terminal output truncation visibility is a
  Lane A surface, unchanged; cross-lane partial stands.
- Three fuzz contracts unwired by design (path normalization, hex paging,
  edit sequences) — lane-e-fuzzing.md.
- Lane B's D2 (localhistory keyed by physical canonicalPath) — untouched,
  owner decision.

## §6 Rebase onto main — conflict ledger

The chain was rebased from `specs-opendal-dispatch` onto `main` (8745341)
after integration: lanes b/c/d rebase onto main itself, a onto b, e onto a,
and the integration branch re-merges the rebased lanes c/d and replays its
unique commits. `main` had advanced 11 commits since the chain forked —
above all multi-root workspaces (#155) — and every conflict below is that
advance meeting a lane's contract. Verification per layer: `go build ./...`,
`go vet ./...`, and the full suite under `CGO_ENABLED=0 go test -count=1 ./...`
all green on darwin/arm64 at each tip before pushing.

### The recurring silent failure mode

Main widened two signatures — `langserver.Starter` gained the workspace
folder list, `search.ListFiles` gained ignore-file options — inside files
the lanes also rewrote. Textual merge resolved those files without markers
while lane tests kept the pre-multiroot call shapes, so the trees built but
`go vet` failed on the test packages. Every such callsite was adapted to
the widened signature with nil/zero arguments, preserving the test's
original assertion, and folded into the commit that introduced it. This is
the class of failure a clean `git rebase` output does not reveal; the vet
pass is what caught it.

### Lane B (3 conflicted files + imports)

- `internal/app/app.go`: both sides canonicalize document paths in
  `openInMode` — main through the multi-root-aware `a.docPath`, lane B
  through `workspace.DocPath` plus the disk-spelling identity. Kept
  `a.docPath` (it is the multi-root superset of the same symlink mapping)
  and added `workspace.DiskSpelling` after it: multi-root identity and
  one-file-one-buffer both hold.
- `internal/app/watch.go`: main joins watch events per workspace folder,
  lane B confines the join. The confined `workspace.JoinRel` now takes
  main's per-folder root — confinement is independent of which root the
  event belongs to.
- `internal/app/codeaction.go` (the lane-B half): same pattern as app.go.
- `internal/search/search.go`: `rg --files` output now passes both lane
  B's `ValidRel` confinement and main's `skipFile` exclusion filter.
- Silent: the `see/internal/workspace` import dropped out of two files in
  resolution; restored and folded into the originating commit.

### Lane A (2 conflicted files + locales)

- `internal/lsp/client.go`: lane A drops `client/registerCapability`
  handling as part of its operation-negotiation contract; main's multiroot
  `workspaceFolderParams` survives in both the initialize params and the
  `workspace/workspaceFolders` answer. Server-side folder awareness and
  the lane's refusal of dynamic registration coexist.
- `internal/app/codeaction.go`: lane A restructures `applyWorkspaceEdit`
  around a canonicalize-once pre-pass and a document-version preflight.
  The rebased shape is lane A's final structure with each
  `workspace.DocPath(a.root, a.realRoot, …)` replaced by the multi-root
  `a.docPath(…)`, then `workspace.DiskSpelling` — the same adjudication
  the original lane merge commit recorded.
- `internal/app/quickopen.go`: import conflict only; main's
  `a.workspacePath` rows made the workspace package import dead, lane A's
  `operr`/`proc` classification imports survive.
- `locales/*.json`: tail-key union (main's beta-feature strings, lane A's
  degraded/crash-budget strings); `catalog_gen.go` regenerated, never
  hand-merged.

### Lane C (config.go is the deep one)

- `internal/config/config.go`: main's beta-feature lifecycle met lane C's
  versioned-degrading contract head-on. The merged shape is lane C's —
  per-key decode, extras write-back, atomic save, backup gate — with
  main's beta machinery threaded through it: `betaFeatures` and the legacy
  vibrancy key are intercepted before the reflection decode,
  `parseBetaFeatures` keeps its lenient in-namespace semantics,
  `migrateBeta` runs at load and its rewrite saves through lane C's
  atomic, backup-gated Save. `stableKeys` survives because beta graduation
  needs to know what the file explicitly said.
- **Spec-vs-main adjudication**: main's
  `TestBetaMigrationIsIdempotentAndDropsMalformedBetaData` dropped a
  non-object `betaFeatures` value outright. SPEC-007 (REQ-CFG-070) forbids
  silently discarding what the file said, so a beta namespace no beta
  shape can represent is now preserved verbatim with a schema_invalid
  issue, exactly as lane C treats every schema-invalid key. The test was
  rewritten to pin this and renamed accordingly; inside the namespace
  (object-parseable but malformed entries) main's lenient cleaning stands
  unchanged. This is the one place the rebase consciously changed main's
  behavior, and it is the spec's requirement, not a preference.
- `internal/config/contract_test.go`/`beta_test.go` both defined
  `writeConfig`; identical helpers, lane C's copy removed.

### Lane D (zero conflicts)

Rebased clean, matching the pre-integration probe. Two test call sites
predated main's widened `ListFiles` signature; adapted as their own commit
with the assertions unchanged.

### Lane E + integration wiring

- `internal/search/search.go`: lane E's ListFiles contract comment kept
  verbatim; main's widened signature is what it documents now.
- `internal/app/find.go` (scan failure sticky + scan freshness): main
  restructured scanning for multi-root — per-root loops, a `filesSeq`/
  `workspaceRootsEpoch` freshness gate, cancellable scans. Lane E's two
  mechanisms survive inside that structure: the sticky error lives in
  `finishQuickFileListing` (`files` nils on error, retry points unchanged),
  and the integration branch's `scanGen` in-flight invalidation moved into
  the same completion function — a scan invalidated mid-flight discards
  its result and re-invalidates ranking exactly as the original commit
  specified, now also covering multi-root scans. Main's own
  staleness probe (`workspace_compat_test.go`) passes the current scan
  generation, which is what the probe observes.
- `locales` in the lane-c re-merge: union, regenerated — same adjudication
  as the original integration merge, re-applied against the rebased lanes.

### What did not survive translation

Nothing intentional. The spec documents themselves (`docs/specs/*.md`,
excluding conformance matrices) are absent from every implementation lane
by construction — they are #168's content, independent of this chain.

## Merged — chain landed on main (2026-11-28)

The full chain was merged into `main` in dependency order, each layer a
squash commit per repository convention:

| PR | Content | Merge commit | Final CI gate before merge |
| --- | --- | --- | --- |
| #168 | spec documents (SPEC-000..009) | `e216f6b` | three-platform green at `f3d214c` |
| #170 | lane D — streaming and range IO | `01b5849` | three-platform green at `7513025` |
| #171 | lane B — resource identity and paths | `8a5627b` | three-platform green at `60b27a0` |
| #169 | lane C — transactional writes, config | `c0171d0` | three-platform green at `be034dd` |
| #172 | lane A — LSP operations, processes | `45029bb` | three-platform green at `ee79bc8` (healed) |
| #173 | lane E — round-2 mechanisms | `2d409a7` | three-platform green at `9f2384f` (healed) |
| #174 | integration — wiring, digests, fuzz | `abf5dea` | three-platform green at `4848959` (healed) |

"Squash" here means each lane's content entered main as a single commit;
the stack's upper layers were healed before merging by merging main into
the lane branch (no history rewritten): conflict regions resolved toward
the integrated tree — this report's own evidence base — while main's own
drift (the multi-root and keyboard-focus fixes that landed after the
chain forked) was preserved. Verified before each push by audit against
the integration tree, and after the final merge by tree equality: main
at `abf5dea` is byte-identical to the integration branch tip `4848959`
whose three-platform CI was green, and `CGO_ENABLED=0 go test -count=1
./...` passes all 29 packages on darwin/arm64 at that revision.

Post-integration platform fixes that landed during the merge window:

- atomicfile's publish boundary tolerates what a Windows reader can hold
  open (`be034dd`, lane C), merged into integration as `cb44fe6`.
- An LSP connection is not done until its last answer is sent —
  `4848959`, fixing the teardown race `TestTypeHierarchyEmptyAndUnsupported`
  exposed on Windows.

Branch archaeology: the `spec-*` lane branches and the `verify-lane-*` /
`reverify-lane-*` chains are intentionally kept as the verification
evidence trail; the conformance matrices above reference them.
