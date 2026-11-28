# Security threat model

This document defines what See protects against, what it deliberately does
not, and which behaviors are conditional. It satisfies REQ-SEC-010 of
SPEC-009, and every claim in it is tied to code that exists — the file is
a description of the system's real boundary, not an aspiration. Where
verification has shown a claim does not yet hold on a supported platform,
the document says so at the claim itself and names the mechanism and
integration state that close the gap; silently scoping claims down or
leaving aspirational ones unmarked are both worse than an honest deficit.
When a new external surface appears (a protocol, an installer, a new
process See launches), this document is part of that change's deliverables
(REQ-SEC-070).

## System model

See is a single-user desktop code reader and editor. It opens a folder the
user chose — the *workspace* — and does four risky things with it:

1. **reads** files in it, including files whose content is adversarial;
2. **executes programs**: language servers, ripgrep, git, task commands
   (`go test` and friends), and build/download tooling;
3. **writes** files in it: edits, replacements, renames, and workspace
   state (local history) beside it;
4. **runs for a long time** with access to whatever credentials the
   user's environment and configuration carry.

The adversary this model cares about is the author of a repository the
user opens but has not decided to trust: a cloned project whose files,
configuration, and tooling hooks are attacker-controlled. The second
adversary is a compromised or corrupted upstream: a download mirror, a
release artifact, or a network peer serving content See installs or
displays.

## Trust boundaries

### The workspace trust decision

See's central boundary is the trust prompt. A workspace is untrusted
until the user explicitly trusts it (`config.Trusted`, keyed by the
workspace path; `internal/config/config.go`). The division of labor:

**Allowed without trust** — because they are passive observation of
content the user asked to read:

- reading and displaying files, syntax highlighting, search over content;
- file watching, directory scanning, git status display;
- local history capture.

**Gated on trust** — because they execute or act:

- language servers do not start (`internal/app/app.go` — documents are
  handed to a server only when `IsTrusted` holds);
- tasks (`go build`, `go test`, …) do not run; invoking one raises the
  trust prompt instead (`internal/app/tasks.go`);
- workspace-aware tooling that would execute workspace-controlled input.

**Never workspace-controlled, regardless of trust** — because no trust
decision should make opening a folder equivalent to running its
installer:

- language server binaries and the search tool are never executed from
  inside the workspace. Every resolver — the built-in
  TypeScript/gopls/clangd/cmake finders, the user-configured server
  finder, and `search.FindRipgrep` — rejects any executable resolving
  inside the workspace root (`internal/languages/languages.go`,
  `internal/languages/configured.go`, `internal/search/search.go`;
  verified by `TestConfiguredServerInsideWorkspaceNeverStarts` in
  `internal/app/security_test.go`). A repository cannot ship
  `node_modules/.bin/evil-ls` plus editor configuration and have See run
  it. Configured servers come from the user's own settings file, not
  from workspace content — there is no workspace-level configuration
  mechanism at all, by design.

  The comparison is by the disk's own spelling, not the lexical shape of
  the path. `filepath.EvalSymlinks` resolves links but not the volume's
  case or Unicode normalization, so on case-insensitive volumes (default
  macOS APFS) a case-variant spelling of a workspace path names the same
  bytes while comparing lexically outside it — the exact evasion this
  boundary exists against. Since the lanes integrated, every resolver
  decides containment through one primitive, `workspace.DiskSpelling`
  probing each component with `os.SameFile` and full Unicode case
  folding: `proc.ValidateExecutable` applies it to every language-server
  candidate, and `search.FindRipgrep` applies the same
  `ValidateExecutable` to its candidates, including symlinks into the
  workspace from anywhere. Verified end to end on case-insensitive APFS
  by `TestValidateExecutableDeniesCaseVariant`,
  `TestDiskSpellingCorrectsCaseVariants`,
  `TestDiskSpellingCorrectsUnicodeNormalization` and
  `TestFindRipgrepDeniesWorkspaceVariants`, each shown to fail when the
  normalization is removed.

### The install boundary

See downloads and installs ripgrep, and builds gopls, into its own
application data directory — never into the workspace.

- ripgrep: the archive **and** its release checksum are fetched; the
  archive is verified against the checksum before anything is written;
  extraction validates the binary is present; installation is staged in a
  temporary sibling directory and atomically renamed into place
  (`internal/app/tools.go`). Behavioral tests pin: checksum mismatch →
  nothing is installed, nothing is left behind, an existing installation
  cannot be overwritten by anything the mirror serves
  (`internal/app/tools_test.go`, REQ-CHK-020/030 evidence).
- gopls: built by the user's own Go toolchain via `go install`, which
  performs the Go module checksum database verification as part of the
  module fetch. See relies on that chain rather than re-implementing it;
  the binary's provenance is the toolchain's responsibility, and the
  pinned version (`goplsVersion`) is part of what See ships.

### The credential boundary

See itself holds no credentials. What flows through it:

- **Configured server environment variables** (`LSPConfig.Env`) may carry
  tokens. See passes them to the server process verbatim. See does not
  log them, echo them, or include them in diagnostics (verified by
  `TestConfiguredServerSecretsStayOutOfTheLog`).
- **Server stderr is untrusted content that may carry those same
  secrets.** The server runs with the user's environment; a crashing,
  misbehaving, or compromised server can echo its environment or
  fragments of it, and the log panel is a paste-into-issues surface.
  Server stderr therefore enters See's log **redacted against the
  process environment** (secret-named variables, long token-shaped
  values, URL userinfo passwords), with redaction applied as bytes
  arrive so a secret split across pipe reads still completes. This is
  a cross-lane decision recorded in lane-e-integration-notes.md §F: the
  redactor is Lane A's (`proc.Tail`), it is the single redaction layer,
  and See's own logging never bypasses it — nor does redaction excuse
  logging secret-shaped inputs in See's own speech. The wiring is
  verified: server stderr pipes through exactly one `proc.Tail` before
  entering exit diagnostics and the log, with no second streaming path
  (`internal/lsp/client.go`; `TestConfiguredServerSecretsStayOutOfTheLog`
  passes unmodified, `TestStartCrashRedactsOversizeStderrSecret` covers
  secrets longer than the capture window byte by byte). What exact-match
  redaction cannot cover is admitted, not hidden: a secret echoed in a
  derived form the environment does not contain (base64-encoded,
  case-folded) still passes, and a process dying mid-print can leak a
  strict prefix of up to len(secret)−1 bytes that never completed into
  the secret.
- **Server stderr and log messages** are attacker-influenced text (the
  server may be compromised upstream). They are stored in a bounded
  in-memory ring (1000 entries × 8192 bytes) and displayed; they are
  never interpreted as commands or markup.

### The content boundary

File content, search results, and process output are all
attacker-influenced bytes. The boundary rule: parsers and display math
over them must not panic, allocate without bound, or produce indexes that
escape the text they point into. Enforcement is layered:

- ripgrep runs with `--max-filesize 4M --max-columns 4000`; result stream
  errors terminate the search rather than yielding half-results;
- the regex playground refuses inputs beyond fixed byte budgets before
  evaluation;
- fuzz targets (`docs/specs/conformance/lane-e-fuzzing.md`) continuously
  probe the slicing, index mapping, and parsing code for panics and
  out-of-range coordinates — and have already caught and fixed three such
  defects.

## In scope

1. **A malicious workspace must not achieve code execution without the
   user's explicit trust decision**, and must never achieve it through
   editor configuration or binary planting, even with trust.
2. **A compromised download mirror must not cause corrupted or
   substituted tooling to be installed or executed**, and must not
   destroy a working installation.
3. **Secrets the user places in server configuration must not leak into
   See's logging** — neither through See's own speech nor through server
   stderr passing into the log (redacted per the credential boundary).
4. **Hostile file content, process output, and scan input must not crash
   the editor or corrupt its display** through memory-unsafe parsing or
   arithmetic.
5. **Partial failures must not impersonate success**: a scan that fails
   is an error, not an empty workspace; a download that fails checksum is
   a rejection, not an installation; a write conflict is a conflict, not
   a silent overwrite.

## Conditional

1. **Resource exhaustion.** See ships built-in bounds — file scan caps,
   search result caps, download size caps, log ring bounds, regex
   playground byte budgets — and those bounds are in scope: bypassing
   *them* is a defect. Beyond them, a workspace that is simply enormous
   (millions of files, multi-gigabyte single files) can degrade the
   editor; the defense is the bound set, not unlimited capacity. Sustained
   external resource attacks (a device filling the disk) are out of
   scope.
2. **Log privacy.** Paths, file names, and content fragments may appear
   in the in-memory log panel — it exists to debug the workspace, and it
   is in-memory only, not persisted. If the user is editing sensitive
   material, the panel's content is as sensitive as the workspace itself;
   Clear is provided. Persisted diagnostics, if ever added, must revisit
   this paragraph before shipping.
3. **Local history privacy.** Snapshots are written `0600` inside a
   `0700` directory within the workspace's own metadata area — private to
   the user's account, but intentionally near the content they protect.
   They are not encrypted; full-disk or account compromise exposes them
   exactly as it exposes the working files themselves.
4. **Trust is per-path.** A trusted workspace whose *content* later turns
   malicious (a pull of adversarial code) is still trusted: trust is a
   user decision about the folder, not a continuous attestation. This is
   deliberate — re-prompting on every change would train users to click
   through.

## Out of scope

1. **A workspace the user explicitly trusts.** Trust is the informed
   consent boundary: a trusted repository can execute arbitrary code
   through tasks and language servers, exactly as it could if the user
   ran its build themselves. See does not sandbox trusted execution.
2. **Host compromise.** A malicious OS, a compromised user account, or a
   tampered See binary invalidates every boundary here.
3. **The upstream of trusted tooling.** See verifies what it downloads
   against the release's published checksum, which means archive and
   checksum are both fetched from the same source: this detects
   corruption and mirror substitution, not a compromise of the upstream
   release pipeline itself. The same applies transitively to the Go
   module checksum database for gopls.
4. **Git hosting authentication.** See shells out to the user's git;
   credentials, signing, and host verification belong to git and its
   configuration, not to See.
5. **What trusted servers do.** A language server the user configured or
   trusted is user-chosen code running with the user's privileges; its
   own behavior is outside See's boundary — including what it writes to
   its own files, its own network peers, and its own logs. What is *not*
   out of scope is what its bytes become once they cross into See's
   surfaces: server stderr entering See's log is in scope (item 3) and
   is redacted, because at that point it is See's logging, not the
   server's.

## Verification and evidence

| Claim | Evidence |
| --- | --- |
| Untrusted workspace executes nothing | trust gates in `internal/app/tasks.go`, `internal/app/app.go`; language servers never start untrusted |
| Workspace binaries never execute, in any spelling the file system equates — containment is decided in the disk's own spelling across every executable resolver | `TestConfiguredServerInsideWorkspaceNeverStarts`; `TestValidateExecutableDeniesCaseVariant`; `TestFindRipgrepDeniesWorkspaceVariants`; resolver rejection in `internal/languages/` |
| Secrets out of See's own logging | `TestConfiguredServerSecretsStayOutOfTheLog` |
| Secrets out of server stderr in the log — single redaction layer, no bypass | `TestConfiguredServerSecretsStayOutOfTheLog` (unmodified), `TestStartCrashRedactsOversizeStderrSecret`, `TestTailRedactsSecretSplitAcrossWrites`; admitted residual: derived forms and strict prefixes of incomplete secrets |
| Checksum mismatch never installs | `TestInstallRipgrepRejectsAnotherChecksum`, `TestInstallRipgrepChecksumMismatchCannotReachTheToolDirectory` |
| Existing installs cannot be displaced | `TestInstallRipgrepChecksumMismatchCannotReachTheToolDirectory`; refusal-to-overwrite in `TestInstallRipgrepInstallsReleaseBinary` |
| Hostile input does not crash parsers | fuzz corpus and regression seeds in `internal/search`, `internal/regexplayground`; LSP frame parser fuzzed per lane-e-fuzzing.md (`FuzzReadMessage`, 6.09M execs clean) |
| Scan failure ≠ empty workspace | `TestQuickOpenRetriesAfterAScanError`, `TestQuickOpenInvalidationRetriesScan`, `TestQuickOpenScanCancellationLeavesNoPartialList` |
