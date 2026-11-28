# Independent re-verification of lane C after the fix round

Re-verifier: an independent session with no access to the fixer's reasoning beyond the committed artifacts. Everything below was re-observed on branch `reverify-lane-c` at fix-tip `120be60` (`fix-lane-c-write-contracts`), Darwin arm64, Go 1.27.4, 2025-11-28. Scope: the fix round `728f7b9..120be60` (three commits) against the original verdict `verify-lane-c` (`1b51aff`). No implementation code was touched; the re-verifier's own probes live in `internal/atomicfile/reverify_test.go` and `internal/config/reverify_test.go` on this branch.

## Verdict: merge-ready

All three must-fix items are resolved with direct evidence: the original verifier's reproductions of the bad behavior were re-run verbatim against the fixed tree and now **fail because the behavior inverted** — the strongest confirmation available. Regression runs are clean twice over (full suite including `internal/app` at 216 s and 225 s, `-race` on both lanes' packages, race count=20 on the atomicfile suite). The F-1 expressibility adjudication survives independent scrutiny and is arguably the only rule that can. New findings are all low severity: one contract-coverage exception (`hiddenNames`), one design wart in the not-yet-wired `CommitIf` API, and stale matrix references. None reopens a must-fix; all are cheap follow-ups listed at the end.

## Cross-check with the original adversarial suite

The verifier's original adversarial tests (branch `verify-lane-c`) were copied unmodified into the fixed tree, run, and removed:

| Original reproduction | Pre-fix meaning | Post-fix result | Interpretation |
|---|---|---|---|
| `TestVerifyZeroValueResetResurrectsInvalidValue` | passed = bug real | **fails** at "control assumption wrong: file has appearance=" | F-1 fixed: the zero-value repair now reaches the file |
| `TestVerifyFailedBackupThenSaveOverwritesOriginal` | passed = bug real | **fails** at "a backup exists after all" | F-2 fixed: the save completed the backup before overwriting |
| `TestVerifyVersionOfWrongTypeIsNormalizedOnWriteBack` | passed = bug real | **fails** at "version rewritten" | F-4 fixed: `"version": "1"` round-trips verbatim |
| `TestVerifySessionNotConcurrencySafe` (with `SEE_VERIFY_RACE_PROBE=1 -race`) | race reported | passes, **no race report** | F-5 fixed: session state is serialized |
| `TestVerifyWriteCallbackPanicLeavesOrphanStaging` | passed = fd/staging leak | passes, log now reads "leaves **0** orphaned staging file(s)" | F-7 fixed: deferred Abort cleans up |
| `TestVerifyNullValuesAreSilentlyDropped` | passed = undocumented erasure | still passes | Behavior unchanged, now deliberately documented (F-3 resolution) — acceptable |
| `TestVerifyBackupAccumulation` | passed = accumulation | still passes | F-8: documented as deliberate in known-gaps item 6 — acceptable |
| `TestVerifyCheckToPublishWindowOverwritesExternalWrite` | passed = window real | still passes | WR-070 inherent limitation, matrix honest about it |
| `TestVerifyRenameFailureCleansStaging` | passed = deviation real | still passes | WR-080 deviation behavior unchanged, as claimed |

## Per-finding verdicts

### F-1 — write-back of zero-value repairs: **confirmed-fixed**

`TestZeroValueResetSticksOnWriteBack`, `TestRepairToNonDefaultSticksOnWriteBack`, and `TestValidationFailureConvergesToInMemoryTruthOnWriteBack` re-run and pass. The original reproduction fails for the right reason (above).

**Independent evaluation of the expressibility adjudication** — the core intellectual question of this round.

The fixer's central claim is that mutation tracking is *state-indistinguishable* in the decisive case. Verified independently: after the degraded load, the in-memory value of a validation-failed key **is** the default, so a user who opens Settings, sees "system appearance" already selected, and closes without touching anything produces no mutation event a dirtiness bit could ever record. The verifier's own fallback sketch ("drop extras when the value differs from the decoded-zero state") therefore cannot heal the file in exactly the scenario the verdict demanded healing for; the only rules that can are (a) blanket convergence of validation-failed keys — what was implemented — or (b) dropping preservation for this class entirely, which is the same thing. The chosen rule is not one option among many; it is the only shape that satisfies the must-fix.

The re-verifier probed whether the rule introduces new problems:

- **Unrequested convergence** (`TestReverifyUnrelatedSaveConvergesInvalidEnumWithoutUserTouchingIt`): hand-written `"appearance": "purple"` + user changes nothing + an unrelated save (in the app: merely opening a workspace triggers `AddRecent`+`saveConfig`, verified at `internal/app/app.go:491–492`) → purple is dropped, the file heals, the warning never repeats. Cost: if the user *intended* the invalid value (a future enum member), it is silently erased on the first save of the session, before any repair gesture. Assessed as **comprehensible and defensible**: the user was shown the invalid-value warning in that session (`validation_failed` issue survives in memory all session), the app visibly ran the degraded default throughout, and the file converging to what the user actually experienced is self-consistent. The alternative — the file contradicting the running app forever — is worse.
- **The downgrade cost is real and correctly disclosed** (`TestReverifyEnumWideningDowngradeLosesNewerValue`): `"appearance": "solarized-auto"` from a newer build + `"version": 99` + unknown keys → save drops the enum value but preserves the version declaration and the unknown keys. Exactly what the matrix claims; the asymmetry (representable values converge, non-representable content survives) is principled because memory cannot hold what it cannot represent, so preservation is the only non-destructive choice there.
- **One incompleteness the matrix does not disclose** (`TestReverifyTypeMismatchNeverHeals`): *type-mismatched* invalid values are preserved forever, so for `schema_invalid` keys the file can never heal to a default through the app — the original F-1 complaint pattern survives for that class. This is the deliberate price of the downgrade protection (a stringly-typed newer value must survive), it is inherent to the expressibility rule rather than a violation of it, but the matrix presents healing as uniform while it is not. Documentation follow-up, not a defect: the user can still repair schema-invalid keys to any non-default value.

Verdict: the adjudication is correct, the matrix discloses the main cost honestly, and the residual (type-mismatch non-healing) is structural, not a regression.

### F-2 — the backup gate: **confirmed-fixed**

`TestFailedBackupBlocksOverwriteUntilBackupSucceeds` and `TestSuccessfulBackupNeedsNoGate` re-run and pass; the original reproduction inverts (above). Implementation review: `Load` holds the original bytes in `unprotected` only when the backup write failed; `Save` completes the backup before the overwrite and refuses — changing nothing — when even that fails. Because backup and target live in the same directory, a save refused for backup failure is one whose own write would have failed anyway, so the gate costs the user nothing in practice while closing the destruction path.

The re-verifier's own variants:

- **User hand-repairs the file between Load and Save** (`TestReverifyGateBacksUpLoadBytesEvenWhenFileChangedSince`): the gate backs up the corrupted bytes Load saw — the only copy it is responsible for — and the overwrite then replaces the user's manual repair. The first half is the gate working exactly right; the second half is the documented no-reload snapshot contract (REQ-CFG-090), not a gate defect.
- **Main write fails after the backup lands** (`TestReverifyGateSurvivesMainWriteFailureAfterBackup`): the pre-save file is byte-identical afterwards, no staging debris; the never-unprotected invariant holds because the backup is complete before the atomic write begins, and the atomic write cannot half-publish.
- **Permissions flipping mid-save** (`TestReverifyGateUnderRacingPermissionFlips`, statistical, 30 iterations of a chmod-flipping racer, also under `-race`): in every iteration either the save succeeded with the original bytes in a backup, or it failed with the original bytes still in the file. No iteration ever lost the only copy.
- **Empty-file edge** (`TestReverifyEmptyFileParseFailureGate`): an empty corrupted config whose backup failed is overwritten with no backup anywhere, because the gate keys on `len(unprotected) > 0` and empty bytes read as nothing-to-protect. Lossless in practice (an empty file carries no settings), confirmed as a behavior fact; a whitespace-only file **is** protected (`TestReverifyWhitespaceFileIsProtected`). Worth one line in the matrix; not a defect.

### F-5 — session threading: **confirmed-fixed**

`Write`/`Commit`/`Abort` serialize on a mutex; the first Commit or Abort ends the session for the rest (pinned independently in `TestReverifyFirstEndingWins`). The original opt-in race probe reports nothing under `-race` now. `-race -count=20` on the full atomicfile suite: pass, 84.8 s. `-race` on the config suite: pass. The threading contract is documented in the package comment and the matrix. One residual observation, not a defect: the mutex makes `Write` calls mutually exclusive but cannot order them, which the documentation states plainly ("which write lands before the commit is the caller's synchronization problem, as with os.File").

### REQ-WR-080 — corrected rationale: **confirmed-fixed (documentation), behavior unchanged as claimed**

The false premise is gone from both `atomicfile.go`'s Commit comment and the matrix. The corrected text names the transient rename failures the verifier catalogued (ESTALE on NFS/FUSE, EINTR, soft-mount EIO, directory-full), and argues the two reasons that survive scrutiny: post-failure publish state is the spec's `Uncertain` — which the spec itself says requires reconciliation rather than blind retry, and the error alone cannot distinguish pre-rename from post-rename failure — plus regenerability of all current callers' bytes. Behavioral equality verified by diff: the publish-failure path is identical to `728f7b9` (sync, close, rename, cleanup on any failure), restructured into the shared `commit()` helper without semantic change. The hazard note for non-regenerable future callers is present in the comment. The Uncertain reasoning correctly cites the spec's own state machine.

### F-3, F-4, F-7, F-8: **confirmed-fixed or acceptably resolved**

- F-3 (null erasure): unchanged behavior, now documented in `MarshalJSON`'s comment and tested as deliberate (`TestNullReadsAsDefaultAndNormalizesOnWriteBack`). The documented reasoning — null carries no information omission does not — is accurate for every current field.
- F-4 (wrongly-typed version): fixed; `"version": "1"` round-trips verbatim, the integer version is not written over it (`TestWronglyTypedVersionStaysPreservedOnWriteBack` passes; original reproduction inverts).
- F-7 (panic leak): `WriteFile`'s deferred Abort closes the handle and removes staging; `TestPanicInWriteCallbackCleansUp` passes and the original probe observes zero orphans.
- F-8 (backup accumulation): documented as deliberate in known-gaps item 6 with a defensible argument (silently deleting backups that may hold the only copy is worse). Accepted.

### F-6 — the fixer's rebuttal is correct, verified independently

`TestReverifyConfigModePreservedOnOverwrite`: an existing config at 0640, 0644, and 0600 each keeps its exact mode through `Save`; a newly created config gets 0600. Mechanism confirmed by code reading: `Begin` copies the replaced file's `Perm()` bits onto the staging file, and `WriteFilePerm`'s 0600 applies only when no file exists. **The original verifier's 0640→0600 claim does not reproduce** — the fixer's refutation stands. Note: the matrix asserts this as "verified" but no test in the fixer's suite exercises it; the re-verifier's test on this branch is now the committed evidence. The setuid/setgid/sticky stripping half of F-6 is real and now documented (`TestReverifySetuidBitStrippedOnReplace`).

## CommitIf / CommitIfAbsent — the cross-lane seam

The fix round added exactly the two shapes the original cross-lane analysis suggested, and did not wire them into the save paths — consistent with the matrix's claim that wiring belongs to the integration phase with the file-observation lane (grep confirms no references outside `atomicfile`).

- **Behavior tests re-run**: the full conditional suite passes, including `TestCommitIfAbsentIsAtomicUnderConcurrency` (exactly one winner among 8 racers). The condition genuinely evaluates after fsync+close, immediately before the rename (`TestCommitIfEvaluatesAtThePublishBoundary` catches a mid-stream external write), which is the REQ-COND-060-conformant shape.
- **REQ-COND-070 degradation honesty: confirmed.** `CommitIf`'s documentation states in plain terms that the condition is not part of the atomic operation, that an external write between cond and rename is still overwritten, and that callers "must treat the semantics as best-effort conflict detection, never as atomic compare-and-publish." Nothing anywhere advertises stronger semantics. `CommitIfAbsent` is honestly documented as the one truly atomic case, and the re-verifier confirmed the mechanism: `link(2)` performs creation and existence-test in one kernel step.
- **Re-verifier probes**: hard-link publish delivers exact bytes and the `BeginPerm` mode with no debris (`TestReverifyCommitIfAbsentPublishesExactBytesAndMode`); a dangling symlink target is *refused* with `fs.ErrExist` while plain Commit replaces it (`TestReverifyCommitIfAbsentRefusesExistingTarget`-equivalent divergence, `TestReverifyDanglingSymlinkCommitVsCommitIfAbsent`) — each half matches its own documentation, though the divergence itself is worth one sentence when the API gets wired.
- **Deadlock question — answered empirically: it is a documentation constraint with no protection.** `TestReverifyCommitIfCondCallingSession`: a cond that calls `s.Write` on its own session self-deadlocks permanently; the goroutine holds the session lock forever and every later operation blocks behind it. `TestReverifyCommitIfSlowCondBlocksAbort` pins the corollary: a racing Abort cannot end the session while cond runs. Assessed as a **design wart, not a blocker**: the lock is not needed for correctness at that point — `finished` is already set, so every concurrent operation would fail or no-op harmlessly if the lock were dropped around cond — which means the deadlock exposure buys nothing. A four-line change (evaluate cond after releasing the mutex) eliminates it entirely. Worth doing before Lane E wires this into the save path; the API is currently unused, so it does not gate this merge.

## Regression evidence

| Run | Result |
|---|---|
| `go build ./...` | clean |
| `CGO_ENABLED=0 go test -count=1 -timeout=10m ./...` (full, run once with only the fixer's tests present, once with this branch's reverify tests included) | **pass, exit 0, both times**; 26 packages ok, `internal/app` 216 s / 225 s, `internal/atomicfile` including the new probes, no flakes |
| `CGO_ENABLED=0 go test -race -count=20 ./internal/atomicfile` | pass, 84.8 s |
| `CGO_ENABLED=0 go test -race -count=1 ./internal/config` | pass |
| Focused re-runs of every test the matrix cites for the fixed rows | all pass |

## Matrix accuracy check

The claim that REQ-CFG-040 and REQ-CFG-070 are now truthfully `implemented` was spot-checked against evidence:

- CFG-040: evidence chain real — gate test exists and passes, harness `TestConfigLoadNoteSurfacesCorruption` exists (`internal/app/config_note_test.go:15`) and passed in the full run. **Implemented claim confirmed**, modulo the empty-file edge above.
- CFG-070: evidence chain real — every cited test exists and passes, and the per-key representability contract is what the code does, **with one exception the matrix does not mention: `hiddenNames`**. It is the struct's only non-omitempty field, so it always emits into the marshaled map and wins every collision: a type-mismatched `"hiddenNames": 42` is **overwritten with the default `.DS_Store` on the first save** rather than preserved verbatim (`TestReverifyHiddenNamesTypeMismatchCollision`, empirically confirmed). The convergence direction is arguably the desirable outcome for a corrupted value — but it contradicts the blanket "values that do not fit their key's type are preserved verbatim" claim in both the matrix and the `MarshalJSON` contract comment (and `issues.go`'s `schema_invalid` doc). Matrix overstatement, negligible user impact, cheap to document.
- **Stale references**: `TestValidationFailurePreservesOriginalOnWriteBack` is cited for REQ-CFG-030 and REQ-CFG-060 but no longer exists anywhere in the tree — it was the pre-fix test whose behavior F-1 inverted. Worse, the CFG-060 row still says of validation failures "its original value survives write-back", which is precisely what the fix round re-contracted away. Two cells need their evidence updated to the new test names.
- All other spot-checked references accurate: the four atomic write paths at `editing.go`/`search_edit.go:381`/`codeaction.go:492` confirmed by reading; the fault-injection helpers unchanged; "neither CommitIf nor CommitIfAbsent is wired" confirmed by grep.

## New findings of this round (all low severity, none blocking)

| # | Finding | Severity | Suggested resolution |
|---|---|---|---|
| R-1 | `hiddenNames` (sole non-omitempty field) is exempt from type-mismatch preservation in practice: `"hiddenNames": 42` is overwritten by the default on first save, contradicting the blanket preservation claim in the matrix, `MarshalJSON`, and `issues.go` | low — hand-corrupted edge, heals in the desirable direction | Document the exception in the matrix/contract, or accept the convergence as uniform-by-design and say so |
| R-2 | `CommitIf` evaluates cond while holding the session lock, which is unnecessary for correctness (the session is already finished for all other operations) and makes a re-entrant cond a permanent self-deadlock | low — API unwired; documented constraint exists | Drop the mutex around cond before the Lane E integration wires it |
| R-3 | Matrix cites deleted `TestValidationFailurePreservesOriginalOnWriteBack` for CFG-030/060, and CFG-060's prose still describes the pre-fix preservation behavior | low — documentation | Update the two cells to the new evidence |
| R-4 | A panic inside cond orphans the staging file (matches documented crash semantics; noted for completeness since `WriteFile` panics were worth fixing) | informational | none needed |
| R-5 | Empty corrupted originals bypass the backup gate (`len(unprotected) == 0`); lossless in practice since empty carries no settings | informational | one matrix line, or `unprotected` as `[]byte` presence flag |

## Bottom line

The fix round resolves every must-fix from the original verdict, with the strongest kind of evidence: the original reproductions themselves invert. The F-1 expressibility adjudication is correct and — given the state-indistinguishability of the decisive repair scenario — the only rule that could satisfy the verdict; its costs are real, disclosed, and bounded. Regression surface is clean. The remaining items are documentation precision (R-1, R-3, R-5) and one API-shape improvement (R-2) that matters before the Lane E wiring, not before this merge.

**merge-ready.**
