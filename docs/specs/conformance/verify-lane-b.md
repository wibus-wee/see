# Independent Verification — Lane B / SPEC-006 (verify-lane-b)

- Verifier: independent verification session, branch `verify-lane-b` (forked at Lane B tip `2fe8b60`)
- Target under verification: `spec-lane-b-paths` tip `2fe8b60` (5 commits over `specs-opendal-dispatch` `f3d214c`)
- Environment: macOS darwin/arm64, Go 1.27.4, APFS (merging) temp volume **plus a real case-sensitive APFS sparseimage** mounted at `/tmp/see-verify-probe/csmnt` for the sensitive-file-system branch Lane B could not execute
- Method: re-ran every evidence test, audited the probe logic line by line, and wrote adversarial tests Lane B did not write. Verifier-added test files, all marked in their headers:
  - `internal/workspace/paths_adversarial_verify_test.go`
  - `internal/app/paths_adversarial_verify_test.go`
  - `internal/search/listfiles_verify_test.go`
  - plus two real-backend probes outside the repo (`/tmp/see-verify-probe/watchprobe.go`, `caseprobe/main.go`) driving the production `mygo.WatchFiles` backend, and a Unicode fold scan (`foldscan/main.go`) covering all 1010 cased BMP runes.

No Lane B implementation code was modified.

## Verdict: **needs-fix** — three small, well-scoped fixes; everything else confirmed

The architecture is sound and the implementer's central claims reproduce. The sensitive-branch probe logic is correct on a real case-sensitive volume, confinement holds against every escape variant I could construct on POSIX, watcher semantics match the real backend, and diagnostics/workspace-edit convergence — which Lane B never directly tested — verifies clean when tested directly. But three defects are real, two of them touch the spec's core promise (one file, one buffer), and one is a potential confinement escape on Windows:

| # | Severity | Defect | REQ |
| --- | --- | --- | --- |
| F1 | medium | `foldKey` uses simple case mapping; file systems full-fold. 29 BMP chars (İ, ẞ, Greek blocks) merge on disk but never converge in identity → two buffers of one file | REQ-PATH-060/080 |
| F2 | medium | D1 is real but **misdescribed**: regular tabs converge fine; *markdown preview* tabs restore unc canonicalized and split the buffer | REQ-PATH-080 |
| F3 | medium — Windows-unverifiable here | trailing-space/dot components (`.. /x`) pass the lexical join but Win32 strips to `..` at open time → lexical confinement does not match filesystem semantics on Windows; exact exploit surface needs Windows verification | REQ-PATH-040 |

### Must-fix before merge

1. **F1** — `internal/workspace/paths.go` `foldKey`: replace `strings.ToLower` with full case folding (`cases.Fold` from the already-imported `golang.org/x/text`). Strictly widens the candidate set; the `os.SameFile` probe keeps it exact. One-line change, and `TestAdversarialFullFoldDivergence` (in-repo, currently failing by design) goes green.
2. **F3** — reject (or document non-support of) components ending in `.` or space in `cleanRel` on Windows builds, or downgrade the REQ-PATH-040 matrix row to "POSIX-verified; Windows semantics unimplemented and unverified". The lexical rule as written does not match Win32 normalization, so the unqualified "implemented" claim is not substantiated for one of the three target platforms.
3. **F2** — the `tab.PreviewTab` (and untitled) restore branch in `restoreSession` must pass through the same `DiskSpelling` normalization as `openInMode` (or construct through it). Currently creates a second document of one file. Alternatively the matrix entry must be rewritten, because its mechanism description is factually wrong (see below).

## Per-REQ verdicts

| REQ | Lane B claim | Verifier verdict | Evidence |
| --- | --- | --- | --- |
| REQ-PATH-010 | implemented | **confirmed** | type roles documented in `paths.go` package comment; display path is render-time only (`RelPath`), no cache key use found in audit |
| REQ-PATH-020 | implemented | **confirmed** | every entry into `a.docs` audited: `openInMode` (all user opens), workspace edit, diagnostics, watcher, quick open, git discard/open. Gaps found are exactly F2 plus the untitled-draft path (keys are synthetic draft paths, out of scope) |
| REQ-PATH-030 | implemented | **confirmed** | table tests reproduce; verifier extended with `%2e%2e` (literal name — correct at this layer), full-width `．．` (literal), `a/b/../../c` (inside), `a/./` — all correct (`TestAdversarialEscapeVariants`) |
| REQ-PATH-040 | implemented | **partial — POSIX confirmed, Windows semantics gap** | POSIX: confirmed, incl. `.. /x` proven to target a literal directory (`TestAdversarialTrailingDotComponents`). Windows: `.. ` strips to `..` at open time (Microsoft normalization semantics; the CPython project documented this exact escape class in 2024). Go only adds the normalization-skipping `\\?\` prefix for paths ≥ 248 chars (`os/path_windows.go` in Go 1.27.4), so typical-length joined paths go through Win32 normalization. Neither implementer nor verifier could execute on Windows; exploit surface analysis in F3 |
| REQ-PATH-050 | implemented | **confirmed** | symlinked-root unification reproduces (`TestRealPathOfSymlinkedRootIsOneDocument`); interior-symlink dual identity reproduces and matches the documented contract. Consequences of dual identity measured — see "symlink dual identity" below |
| REQ-PATH-060 | implemented | **partial — defect F1** | the probe logic is correct (verified on a real case-sensitive volume: fold-equal candidates never adopted, exact spellings round-trip — `TestAdversarialSensitiveVolume` with `SEE_VERIFY_CS_VOLUME`). But the fold *table* is wrong: `strings.ToLower` is simple case mapping, file systems full-fold. Verifier scan of all 1010 cased BMP runes: 29 merge on APFS but are invisible to `foldKey` (U+0130 İ, U+1E9E ẞ, U+1F88–U+1FAF, U+1FBC, U+1FCC, U+1FFC); zero false merges in the reverse direction |
| REQ-PATH-070 | implemented | **confirmed** | URI codec handles `%`/`%25`, spaces, `?`, `#`, CJK, mixed unicode in real files (`TestRealFileRoundTrip` passes). Audit found exactly one decode point (`uri.ToPath`); `document_links.go`'s second `PathUnescape` is markdown-link syntax, not file-URI decoding — correct. `%2e%2e` decodes to a literal `..` *file name inside an absolute path*, never a traversal, because URI decode yields absolute paths and `JoinRel` never sees them |
| REQ-PATH-080 | partial (D1/D2) | **partial confirmed — but D1's mechanism is misdescribed and the consequence is worse than stated** | verifier directly tested what Lane B only implied: diagnostics for a case-variant URI land on the one open document in both buffer-backed and disk paths (`TestAdversarialDiagnosticsLandOnCanonicalDocument` PASS); workspace edit of an unopened variant writes the real file with no phantom (`TestAdversarialWorkspaceEditCaseVariantUnopenedMergingFS` PASS). D1/D2 assessment below |
| REQ-PATH-090 | implemented | **confirmed with a quantified hazard** | in-app rename continues identity (PASS); watcher rename closes old, opens none (PASS). The user-facing consequences are documented below under "watcher rename semantics" |

## Defects in detail

### F1 — fold table mismatch splits buffers for 29 characters (medium)

**Repro** (in repo, failing by design): `go test ./internal/workspace/ -run TestAdversarialFullFoldDivergence`

```
fs merges İ / i+dot-above: true; FoldEqual: false
DIVERGENCE CONFIRMED: the filesystem merges "İtest.txt" and "i̇test.txt" into one file,
but DiskSpelling("i̇test.txt") does not converge
```

**Mechanism**: `foldKey` = NFC + `strings.ToLower` (simple case mapping). APFS and NTFS fold with the full Unicode table. Where full folding expands one char into two (İ→i+U+0307, ẞ→ss, Greek capitals with prosgegrammeni→two chars), the filesystem merges the spellings but `foldKey` does not, so `DiskSpelling` finds no candidate and the two spellings become two documents of one file. The verifier's exhaustive scan (`foldscan`, all 1010 cased BMP runes, each probed against the real filesystem): **29 divergence characters, 0 reverse-direction false-merge risks**.

**Impact**: user opens `İzin.txt` in the tree and a language server or CLI sends `i̇zin.txt` — two buffers, edits diverge, save-conflict is the only backstop. Rare characters but real languages (Turkish İ, German ẞ in uppercase-sharp-s names).

**Expected**: full folding in `foldKey` (e.g. `cases.Fold`); since the SameFile probe is the actual arbiter, a wider candidate net cannot produce false merges — verified in both directions.

### F2 — D1 misdescribed: regular tabs converge, markdown preview tabs split (medium)

**Repro** (in repo, failing by design): `go test ./internal/app/ -run TestAdversarialMarkdownPreviewRestoreSpelling`

```
restored docs: stale-key present=true canonical-key present=false
BUFFER SPLIT CONFIRMED: the same file is open as two documents ("…/A.md" stale, "…/a.md" fresh)
```

**What the matrix claims**: "openInMode 只在 tabs 命中时才 canonicalize;session 恢复时 tabs 为空直接采用存储拼写" — i.e. the canonicalization is conditional. **This is factually wrong**: `openInMode` calls `DiskSpelling` unconditionally, and regular tabs restore through `openIn` → `openInMode`. The verifier's `TestAdversarialRegularTabRestoreConverges` PASSes: a regular tab stored as `A.txt` and case-renamed to `a.txt` between sessions restores converged.

**What actually happens**: only the `tab.PreviewTab` restore branch (markdown preview tabs) creates the `doc` directly from the stored spelling, bypassing `openInMode` entirely. Consequence is worse than the matrix's "读写仍指向同一文件,不影响正确性": reopening the file by its disk spelling creates a **second buffer of the same file** — the exact failure SPEC-006 exists to prevent — albeit behind a narrow trigger (markdown preview tab + case rename between sessions, or a legacy session storing a non-canonical spelling).

**Severity assessment vs implementer's "影响低"**: the trigger surface is narrower than they believed, but the consequence is buffer split, not cosmetic staleness. Net: **low-medium, not negligible**. Given the trivial fix, should-fix before merge.

### F3 — Windows trailing-dot/space components defeat the lexical confinement (medium; Windows-unverifiable here)

**Repro reasoning** (cannot execute on macOS; evidence chain): `JoinRel(root, ".. /evil.txt")` passes `cleanRel` — `.. ` is not `..`. On Windows the path is opened without the `\\?\` prefix whenever the full path is under 248 chars (verified in Go 1.27.4 `os/path_windows.go`: `fixLongPath` returns the path unchanged for `CanUseLongPaths` or short paths). Win32 normalization strips trailing spaces and dots per component at open time, so `.. ` resolves as `..` and the write escapes the root. Microsoft documents that canonicalization and comparison cannot be done naively for exactly this reason, and CPython issue #157504 documents the same escape class in production.

**Exposure analysis on Windows** (static; the verifier has no Windows machine — this is why the row must be downgraded rather than asserted): the clearest victim is `submitEntry`, where typed input like `.. /evil.txt` creates outside the folder the prompt claims to confine to — user-driven, so low security value, but straight spec non-conformance. The destructive path `discardGitChanges` → `trash(joined)` for untracked files is blunted by two facts the verifier checked: untracked files with trailing-space/dot names cannot exist on a Windows disk in the first place, and git's default `core.protectNTFS=true` refuses checkout of such names from malicious repos — so the git vector is uncertain and needs a Windows machine to settle. The correct verdict is therefore not "escape proven" but: **the lexical rule does not implement Windows path semantics, the conformance matrix row claims implemented without qualification, and neither implementer nor verifier can run the Windows experiment** — the row must be fixed (POSIX-verified) or the rule hardened (reject trailing `.`/space components on Windows builds, a few lines in `cleanRel`).

**POSIX verdict**: confirmed safe — `.. ` is a literal directory name, and the verifier proved a join into it reads only the literal directory (`TestAdversarialTrailingDotComponents`).

### F4 — case-only rename + stale tab = silent overwrite, no conflict anywhere (low-medium, contract-shaped)

**Repro** (in repo, passing documentation tests): `TestAdversarialExternalCaseRenameReopenSplit`, `TestAdversarialWatcherMissedCaseRenameSplits`

```
saving the stale tab: saved=true; A.TXT now "edit in stale tab\n" while the fresh tab shows "content\n"
CONFIRMED HAZARD: silent overwrite of the file the fresh tab displays, no conflict anywhere
```

After an external case-only rename (which the real macOS backend *does* report — verifier-confirmed via `caseprobe`), the old tab is `gone` and the tree offers the new spelling. If the user edits and saves the stale tab, `saveDoc`'s byte comparison reads the old path — which on a merging filesystem **still resolves to the same file** — the comparison passes, and the write goes into the renamed file invisibly, while the user's other tab of the same file still displays the old content. This is a consequence of the (industry-standard, defensible) watcher-rename contract interacting with a merging filesystem; it is not a regression and VS Code class-shares it. Worth one sentence in the conformance matrix; a future fix would detect spell-change or link the identities at save time.

### F5 — dual-identity stale views are silent until save (low, contract consequence, undocumented in matrix)

**Repro**: `TestAdversarialSymlinkDualIdentityStaleView` (PASSes as documentation)

The verifier drove the **real** mygo watcher (`watchprobe`): a write to `real.txt` produces exactly one event, `write real.txt` — never anything at `link.txt`. So of the two logical identities of one file, only the target spelling reloads; the symlink view goes stale with no marker. Byte-compare save protection then refuses with a conflict prompt: **no silent data loss possible** (verified: save through the stale symlink identity is refused, external bytes intact). This is conformant to the documented dual-identity contract — but the matrix does not mention the staleness consequence, and "各自内部一致" overstates what the user experiences.

### F6/F7 — robustness notes (trivial)

- `sub/..` as a watcher FileHint/FileRemove path lexically resolves *to the root* (`JoinRel` returns root for canceling input — correct per its contract), so a malformed hint reloads or mark-gones every document (`TestAdversarialWatcherHintEscapeIsHarmlessButUncaught`). Real watcher streams never contain `..`; no escape possible. Optional hardening: reject cancel-to-root inputs for destructive event kinds.
- `joinRoot` degrades malformed quick-open entries to *the root directory* rather than an error — clicking such a row tries to open the root as a file. Benign but sloppy (`TestAdversarialQuickOpenJoinFallback`).

## Independent assessment of the declared D1/D2 residuals

- **D1**: real, mechanism misdescribed, consequence understated — see F2. Implementer's severity "影响低" is too optimistic; the correct statement is "narrow trigger, real buffer split".
- **D2** (localhistory keyed by physical path): verified by code audit — `canonicalPath` uses `EvalSymlinks`, no case folding. Given the app passes canonical disk-spelling logical paths, case keys are consistent; the only collision is interior-symlink dual identities sharing one history. Content is identical bytes, restore either way is correct, so the implementer's "影响低" is **accurate**. No action needed beyond documentation.

## Watcher rename semantics — the assigned deep-dive

**Question: does the watcher-rename contract lose unsaved user data? Answer: no — verified end to end.**

`TestAdversarialWatcherRenameOfDirtyDocument` (PASS): a document with unsaved edits whose file is externally renamed keeps its buffer byte-for-byte, marked `gone`, path unchanged. Saving is refused because the old path no longer exists (`saveDoc` treats the read failure as a conflict); the user must explicitly overwrite, which recreates the file at the old spelling. The refusal prompt is arguably misleading (the file did not "change on disk", it moved), but nothing is lost silently — the buffer, the recovery cache, and the conflict flag all preserve the bytes. This matches mainstream editor semantics (VS Code classifies external renames the same way).

**Real-backend facts the verifier established** (these underpin the whole rename contract and were previously assumed, not measured):

| scenario | real macOS FSEvents via mygo | consequence |
| --- | --- | --- |
| atomic save (write tmp + rename over) | `write` of the **target**, not a rename | unmodified docs reload correctly — matrix's model holds in production, not just in tests (`TestAdversarialAtomicSaveReplacementReloads` PASS) |
| direct write | `write target` | reloads; symlink identities of the same file do not (F5) |
| rename `a` → `b` | `rename` with oldpath/newpath | old identity closes, contract as documented |
| **case-only rename** | **reported as a normal rename** | old identity closes (F4's flow is reachable in production) |
| NFC→NFD normalization rename | reported as rename (plus a create) | same |

## Out-of-scope change review: `internal/search/search.go` ValidRel filter

**Verdict: benign, protective, no legitimate file dropped — acceptable to keep, though it is ownership-crossing.**

Empirical evidence (`listfiles_verify_test.go`): files named with spaces, `%`, CJK, trailing dots, semicolons, quotes, and backslashes all survive the filter into the quick-open list. A newline in a filename fragments across scanner lines into phantom entries — **pre-existing behavior identical before the lane** — but the fragment can no longer be joined into an escape, which is exactly what the filter adds. The only files excluded are those rg itself hides (dotfiles) — unrelated to the filter. The claim "合法但怪异文件名被静默吞掉" does not hold: no legitimate POSIX filename can fail `ValidRel` (NUL and `/` are impossible in components; a file literally named `..` cannot exist). Note the inverse did hold before the lane: a fragment line of `..` would have joined outside the root.

## Matrix accuracy notes for the implementer

1. REQ-PATH-060 row claims the merge branches were "真实执行" — true but incomplete; the fold table itself is narrower than the filesystem's (F1). The row should be partial until fixed.
2. D1's mechanism description is wrong (see F2); the deviation text should be rewritten either way.
3. The symlink dual-identity staleness consequence (F5) deserves one line in the matrix — it is the contract's actual user-visible cost.
4. "场景 7(display 不改键)由契约结构保证" — acceptable; verifier audit found no counterexample.

## Test verification inventory (all re-run by the verifier, none trusted)

- `CGO_ENABLED=0 go test ./internal/workspace/ ./internal/uri/ -race -v` — **all PASS** at Lane B tip; after verifier test additions the only failure anywhere is the intentional F1 demonstration in workspace
- `go test ./internal/app/` for all 10 matrix evidence tests — **all PASS**; same set under `-race` — PASS, **zero data races**; the only failure is the intentional F2 demonstration
- `go build ./...` — clean; `go vet` on touched packages — clean
- full `go test ./...` at Lane B tip (before verifier test additions) — **all 25 packages PASS**, matching the matrix's claim (227s for internal/app); `go tool mygo vet .` — clean
- real-sensitive-volume run: `SEE_VERIFY_CS_VOLUME=/tmp/see-verify-probe/csmnt go test ./internal/workspace/ -run TestAdversarialSensitiveVolume` — PASS (volume confirmed case-sensitive: `foo.txt`/`Foo.txt` coexist)
- NOTE: on this verify branch `go test ./internal/workspace/ ./internal/app/` has exactly two failing tests — `TestAdversarialFullFoldDivergence` and `TestAdversarialMarkdownPreviewRestoreSpelling` — both intentional defect demonstrations (F1, F2), marked as such in their file headers.

## Engineering hygiene

- 5 commits, each scoped as claimed; `go.mod` diff is only the `x/text` direct-dependency promotion plus removal of the unreplaced `mygo` sum lines — both explained by the actual imports. No unrelated entrainment found besides the declared `search.go` change (reviewed above).
- Comments in the new code are accurate about mechanism (the probe comment about sensitive-file-system behavior is exactly what the verifier confirmed on real hardware).
- One cosmetic note: `DiskSpelling`'s doc comment claims a "fast path" for exact spellings; the implementation probes every component regardless. Harmless on local disks; on network filesystems every open pays full `ReadDir` scans of each ancestor. Not a conformance issue.
