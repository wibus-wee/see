# Independent adversarial verification of lane C (SPEC-003 / SPEC-007)

Verifier: an independent session with no access to the implementer's reasoning beyond the committed artifacts. Everything below was reproduced by the verifier's own runs on branch `spec-lane-c-writes` (tip `728f7b9`), Darwin arm64, Go 1.27.4, 2025-11-28. The implementer's claims counted for nothing unless re-observed.

Verification method: read the specs and the implementation, re-ran every evidence suite, hand-verified the fault injections were genuine, and wrote eight additional adversarial tests probing scenarios the implementer's suite does not cover (`internal/atomicfile/adversarial_verify_test.go`, `internal/config/adversarial_verify_test.go` — new files on this branch, no implementation code touched).

## Verdict: needs-fix

The transactional-write half is solid: the core claims reproduce exactly, including kernel-level fault injection, and no adversarial scenario refuted any WR requirement beyond what the matrix itself documents. The configuration half has **two real contract violations** the implementer's matrix overstates as implemented: REQ-CFG-040 (the backup gate has no enforcement) and REQ-CFG-070 (zero-valued repairs are silently overwritten by preserved invalid values, and `null` values are silently erased). Both are confirmed below with reproductions. Neither is large; both are cheap to fix. The divergent REQ-WR-080 adjudication is *permissible with corrected reasoning* — the implementer's stated premise is factually false, though the decision itself survives scrutiny.

## Re-runs of the implementer's evidence

| Suite | Result |
|---|---|
| `CGO_ENABLED=0 go test -race ./internal/atomicfile ./internal/config` | pass (all 10 atomicfile tests incl. fault injections; all config tests) |
| `go test ./internal/localhistory ./internal/search` | pass |
| Focused harness run: `TestSaveFailureReportsErrorAndKeepsOldBytes`, `TestRecoveredEditsRefuseToOverwriteExternalChanges`, `TestUntitledSaveKeepsEditsMadeBeforeCompletion`, `TestUntitledSaveOverwritesNativePanelDestination`, `TestAtomicWriteFailureLeavesNoPartialDestination`, `TestConfigLoadNoteSurfacesCorruption`, `TestEditorFontSizeSaveFailure`, `TestEditorFontSizeCommands` | all pass |
| Full `./internal/app` suite, `-timeout=10m` | pass, 213.8s, exit 0 |
| `go build ./...`, `go vet` on both lanes' packages | clean |

Fault-injection genuineness verified independently by compiling the test binary and running the helpers by hand:

- `SEE_ATOMICFILE_HELPER=limited` exits **3** (the write-error path, not the setrlimit-failure path 2 and not vacuous success 0), and the target directory contains no staging — the EFBIG path really exercises `WriteFile`'s error handling.
- `SEE_ATOMICFILE_HELPER=crash` + SIGKILL: the orphaned staging file `.crash-target.txt.<hex>.tmp` remains with the uncommitted bytes inside, dot-hidden, while the target still holds `oldtruth`. Confirmed: orphans never impersonate the target.

## Per-requirement verdicts

### SPEC-003 — transactional write lifecycle

| REQ | Matrix claim | Verdict | Verifier evidence |
|---|---|---|---|
| WR-010 | implemented | **confirmed** | `Write`/`Commit` separation reads correctly; `TestCommitFailureDoesNotReportSuccess` passes; `WriteFile` contract documented. |
| WR-020 | implemented | **confirmed** | 997-byte-piece streaming test passes; partial-accept semantics of `Write` documented. |
| WR-030 | implemented | **confirmed** | Harness `TestSaveFailureReportsErrorAndKeepsOldBytes` passes; commit error path never swallows (code-read + test). |
| WR-040 | implemented | **confirmed** (one caveat) | Poisoned-session test passes. Caveat: the state machine is only correct for single-goroutine use, which is nowhere documented — see defect F-5. |
| WR-050 | implemented | **confirmed** | Abort/failure cleanup tests pass; EFBIG run observed no staging residue. |
| WR-060 | implemented | **confirmed** | All four paths verified by code inspection: `saveDoc` (editing.go: `atomicfile.WriteFile`), `saveUntitled` (via `writeFileAtomically` delegate), search edits (search_edit.go:381), `applyEditsToFile` (codeaction.go:492). Baseline `os.WriteFile` truncation is gone from all of them. Directory-fsync omission is an implementation choice the spec allows; mode-preservation test passes. |
| WR-070 | partial | **confirmed partial — window reproduced, and it is larger than implied** | My `TestVerifyCheckToPublishWindowOverwritesExternalWrite` constructs the exact adversarial case: conflict check passes, then an external write lands *entirely inside the staging stream* — it is silently overwritten. The window is not a microsecond race at the rename: it spans **the entire staging write** (measured 4.5 ms / 5 ms / 27 ms for 10 KB / 1 MB / 50 MB on local NVMe; longer on network volumes or large files). Conflict identity is full-content byte comparison (strongest possible, so a same-size same-content rewrite is correctly not a conflict; any other content in the window is silently lost). The matrix's *partial* status is honest; note for accuracy that the window scales with file size, which the matrix does not say. |
| WR-080 | divergent | **deviation permitted, premise refuted** — see adjudication below |
| WR-090 | implemented | **confirmed, strengthened** | Implementer's test only reads after all writes finish. My `TestVerifyConcurrentReadersNeverSeeTornContent` ran 4 writers streaming in 1500-byte pieces while readers polled continuously: **1660 concurrent reads, every one a complete version**. No torn content is observable at any instant. Last-writer-wins documented in the package comment. |
| WR-100 | implemented | **confirmed** | SIGKILL helper verified by hand (above): orphan staging dot-hidden, target intact, fresh write succeeds. |
| WR-110 | partial | **confirmed partial** | Mode preservation test passes. xattr loss reproduced on Darwin (`xattr -w` marker gone after rename-replace). Hardlink break and dangling-symlink replacement reproduced in my tests. All match the documented limitations. Undocumented extras found: setuid/setgid/sticky bits are silently stripped on replace (`Perm()` masks them); `config.Save` does *not* preserve the existing config file's mode (0640 → 0600 on overwrite — not a regression, the baseline did the same, but inconsistent with the general contract and undocumented). |

### SPEC-007 — versioned configuration

| REQ | Matrix claim | Verdict | Verifier evidence |
|---|---|---|---|
| CFG-010 | implemented | **confirmed** | Per-field decode, documented domains, unknown-key policy all present and tested. |
| CFG-020 | implemented | **confirmed** (one edge) | `TestNewerVersionDegradesWithIssue` passes; write-back keeps version 99. Edge: a wrongly-*typed* version (`"version": "1"`) is preserved in extras yet silently normalized to int on write-back because MarshalJSON always emits the key first — see F-4. |
| CFG-030 | implemented | **confirmed** | All five issue kinds test-observed; missing-phase reasoning (no migration/no second source exists) verified by inspection. |
| CFG-040 | implemented | **REFUTED in the backup-failure path** | Main path confirmed (backup + untouched original + degraded defaults + startup note, harness test passes). But `TestVerifyFailedBackupThenSaveOverwritesOriginal` demonstrates: when the backup write itself fails, the next `Save` destroys the user's original file with **no backup anywhere** — the spec's overwrite gate ("only after user confirmation or automatic backup completes") has no enforcement. See F-2. |
| CFG-050 | implemented | **confirmed** | `internal/cli` verified: open-path arguments only, single source. |
| CFG-060 | implemented | **confirmed** | Enum/range validation tests pass; per-field degradation contract verified. |
| CFG-070 | implemented | **REFUTED in two edge paths** | Main unknown-key round-trip confirmed. But: (1) `TestVerifyZeroValueResetResurrectsInvalidValue` — every struct field is `omitempty`, so repairing an invalid value *to the default* emits nothing, and the preserved invalid value wins the write-back: the user's repair is silently overwritten, the file can never self-heal, and the warning never clears. (2) `TestVerifyNullValuesAreSilentlyDropped` — explicit `null` values unmarshal without error into Go zero values, are neither preserved nor validated, and are silently erased on the first write-back. See F-1, F-3. |
| CFG-080 | implemented | **confirmed** | No project-config mechanism exists anywhere in `internal/` (grep-verified); the documented standing contract for future work is appropriate. |
| CFG-090 | implemented | **confirmed** | Documentation-only claim, accurate: single snapshot, no reload path exists. |
| CFG-100 | implemented | **confirmed** | Honest "no migration exists" recording; backup machinery is tested on the parse path. |

## Defects found (not in the implementer's matrix)

### F-1 — Repairing an invalid config value to its default is silently overwritten by the invalid value (medium)

Every `Config` struct field is tagged `omitempty`. `MarshalJSON` decides collision winners by key presence in the marshaled struct — so a repair to the zero value (the valid default: `""` appearance = system, `0` font size = 13, `false` = feature on) emits nothing, and the preserved invalid value in `extras` is written back instead.

Reproduction (`internal/config/adversarial_verify_test.go`, passes — i.e. the bad behavior is real):

```go
// file contains {"appearance": "purple"}  (invalid enum)
c := Load(p)          // validation_failed, Appearance = "" in memory, "purple" preserved
c.Appearance = ""     // the user repairs to the valid default: system appearance
c.Save()              // file still says "purple" — the repair never reaches disk
```

Consequences: the file can never be repaired to a default value through the app; the startup warning reappears on every launch forever even though the in-memory state is already the intended one; and if a future build makes `"purple"` valid, the user's file silently changes meaning. The exact violation of the documented invariant "once the key validates again, the typed value is the configuration" — the zero value *is* a valid typed value but cannot win.

### F-2 — Failed backup means the next save destroys the original configuration with no backup anywhere (medium severity, low probability)

`decode` treats backup failure as a note (`Backup` stays empty) and nothing more. The spec's REQ-CFG-040 gate — overwrite only after user confirmation or completed backup — is not enforced anywhere.

Reproduction (passes — bad behavior real): corrupted config; directory made unwritable at load time (disk full, transient permission failure) so the backup write fails; directory recovers; one settings change → `Save` atomically replaces the user's unrecoverable original. No backup exists. Window: config corrupted + backup-target write failure at startup + recovery before first save.

### F-3 — Explicit `null` values are silently erased on write-back (low)

`{"editorFontSize": null}` unmarshals into `float64` without error, leaving zero. No schema issue, no preservation — and `omitempty` erases the key on the next write-back. Strictly a REQ-CFG-070 violation (the file said something the write-back silently drops). Marginal in practice: `null` semantically means "unset" for most users, and the resulting behavior equals the default. Either preserve-and-report or document the normalization.

### F-4 — Wrongly-typed `version` is silently normalized despite being preserved (low)

`"version": "1"` correctly raises `schema_invalid` and is preserved in extras — but `MarshalJSON` always writes the integer `version` first, so the preserved original loses the collision and the file is silently normalized to `1`. Inconsistent with the contract applied to every other key; harmless in practice.

### F-5 — Session state machine races under concurrent use; threading contract undocumented (low)

`SEE_VERIFY_RACE_PROBE=1 go test -race -run TestVerifySessionNotConcurrencySafe` reports a data race between `Session.Write` (atomicfile.go:119, read of `s.open`) and `Session.Commit` (atomicfile.go:144, write). Concurrent use of one session is arguably outside the intended contract — but nothing documents that, and the `io.Writer` shape invites the assumption. Document single-goroutine use explicitly (Go's own `os.File` is safe, which makes the undocumented contrast sharper). The probe is opt-in so the branch suite stays green.

### F-6 — Sticky/setid bits silently stripped on replace; config file mode normalized to 0600 (low, documentation)

`Begin` copies `info.Mode().Perm()`, which excludes setuid/setgid/sticky — silently dropped for such files. Also: `config.Save` uses `BeginPerm(0600)`, so overwriting an existing 0640 config tightens it to 0600. The second is harmless-to-beneficial and matches baseline behavior, but both are metadata consequences REQ-WR-110's documentation does not mention.

### F-7 — Panic in the write callback leaks the staging file descriptor (low, consistent-with-docs)

`WriteFile` has no deferred cleanup: a panic in the caller's callback abandons the staging file *and* its open handle (the orphan-on-disk part matches documented crash semantics; the fd leak until process exit is the small extra). Confirmed by `TestVerifyWriteCallbackPanicLeavesOrphanStaging`.

### F-8 — Backup files accumulate forever (low, hygiene)

Every corrupted launch adds one `config.json.broken-<timestamp>`; nothing ever removes them (verified: 3 loads → 3 backups). Config-directory-sized, so bounded in practice.

## Independent adjudication: REQ-WR-080 divergent

Implementer's premise: *"a same-directory rename that failed failed for configuration reasons (permissions, a directory in the way) that retrying the same rename cannot fix"* — hence publish failure always cleans staging.

**The premise is factually false.** Transient rename failures exist on POSIX:

1. **NFS/FUSE cache-staleness `ESTALE`**: the Linux kernel community explicitly documents that rename can fail with ESTALE purely from client-side cache races and that *retrying the identical operation is the correct remedy* (Jeff Layton, LKML 2012; Red Hat bug 1609210 shows a real retry-succeeds case). Kernels since ~3.x retry internally, narrowing but not eliminating userspace exposure.
2. **`EINTR`**: signal interruption before completion — the classic retry case on any filesystem.
3. **Soft-mount `EIO`/`ETIMEDOUT`** during server unavailability: the same rename succeeds after recovery.
4. **ENOSPC/EDQUOT on directory growth**: a rename creating a new entry in a full directory can fail transiently and succeed after space frees.
5. The implementer themselves acknowledges the Windows sharing-violation case — transient and retryable — and only declines it as untestable from Darwin.

There is a subtlety that partially rescues the decision: after `EINTR`/network uncertainty, the rename *may have already happened server-side*, so a retry hits the spec's own `Uncertain` state ("requires reconciliation, no blind retry"). Retained staging is only safely republishable when the failure is known-pre-rename — which is exactly what cannot be distinguished cheaply.

**Adjudication: deviation PERMITTED, but the documented reasoning must be corrected.** The behavior satisfies both MUSTs (permanent errors clean staging; random per-session names make cross-session reuse impossible — verified by code and the crash test). The SHOULD is a legitimate engineering trade-off *for different reasons than stated*: (a) for all four current callers the staged bytes are regenerable at near-zero cost, so in-place retry buys nothing; (b) post-failure rename state is uncertain on network filesystems, so retention rarely enables a safe retry anyway. The false universal claim ("publish failures are configuration failures") should be removed from `atomicfile.go`'s Commit comment, since it will mislead the next reader — and it matters practically if `atomicfile` is ever reused for non-regenerable content, where unconditional cleanup destroys the only copy (demonstrated: `TestVerifyRenameFailureCleansStaging` — the staged bytes are gone).

## Cross-lane interface: atomicfile vs Lane E's SPEC-009-C conditional operations

`internal/fileobs` does not exist yet on `spec-lane-e-round2` (branch contains no such package), so this assessment is about API headroom:

- `atomicfile.Session` exposes `Write/Commit/Abort` with **no conditional-publish hook**. `Commit()` is unconditional `fsync`+`close`+`os.Rename`. A caller can only check-then-commit from outside — precisely the `stat` + unconditional mutation that REQ-COND-050 forbids advertising as conditional.
- Integration is nonetheless cheap in one direction: the session shape (explicit staging lifetime, publish as a discrete final step) is exactly where REQ-COND-060 wants conditions evaluated. Adding `CommitIf(cond func() error)` that evaluates immediately before the rename would be a small additive change, would shrink the saveDoc TOCTOU window from the whole staging duration to the rename boundary — but cannot close it: POSIX rename carries no condition, so per REQ-COND-070 the result must be advertised as best-effort-degraded, not atomic-conditional.
- One fully-atomic case is available and currently unsupported: **create-if-absent** via `link(2)` (fails with EEXIST atomically). Worth exposing as a publish mode rather than letting Lane E duplicate the staging machinery.
- Bottom line: Lane C did not paint Lane E into a corner, but Lane E must extend `atomicfile` rather than compose it. Suggested seam: `CommitIf` (best-effort, documented) + `CommitIfAbsent` (link-based, truly atomic). Neither exists today.

## Matrix accuracy notes (not defects)

- REQ-WR-070 partial is honest but undersells the window size: it scales with file size (the whole staging duration), not just the rename instant.
- "21 REQ → 18/2/1" recount verified correct.
- All file references in the matrix (`codeaction.go:492`, `search_edit.go:381`, etc.) verified accurate.
- No unrelated content in the three commits; `catalog_gen.go` regeneration is consistent with the locale JSONs.

## Must-fix list for merge-readiness

1. **F-1** — write-back must distinguish "user set the zero value" from "field untouched". Minimal correct fix: drop preserved extras for a key whenever the struct field has been *mutated since load* (needs a dirtiness bit per validated field), or — simpler and nearly as correct — drop the extras entry whenever the field's current value differs from the invalid original's decoded-zero state, plus documentation. Any fix must make `TestVerifyZeroValueResetResurrectsInvalidValue`'s scenario round-trip correctly.
2. **F-2** — enforce the REQ-CFG-040 overwrite gate: when `parse_failed` and backup failed (`Backup == ""`), `Save` must refuse (or require explicit confirmation) rather than overwrite the unprotected original.
3. Correct the REQ-WR-080 rationale in `atomicfile.go`'s Commit comment (and the conformance matrix): remove the false "rename failures are permanent" premise; keep the deviation with the cost/uncertainty argument.

Recommended, not blocking: F-3/F-4 documentation or handling, F-5 threading-contract documentation, F-6 documentation, F-8 backup rotation. F-7 is acceptable as-is.

Everything else in lane C — the write primitive, the four converged paths, the fault-injection evidence, the conflict-check honesty, the config load pipeline, and the conformance matrix itself — is confirmed as claimed.
