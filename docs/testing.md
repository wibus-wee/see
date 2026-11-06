# Testing See

Run `./scripts/verify.sh` from a graphical-development machine with Go, Git,
ripgrep, gopls and Python 3 installed. It runs the Go suite, MyGo's UI lifetime
checks, and the changelog tests. `SEE_RG` and `SEE_GOPLS` may select absolute tool
paths; otherwise the script resolves them from PATH before the app harness
isolates its home directory. Missing integration tools fail this command instead
of silently reducing the checks.

The PR workflow runs this command on Linux, macOS and Windows with gopls
v0.24.0. The Python release fixtures run on Linux/macOS because they execute a
POSIX shell script. Linux also runs race checks for the concurrent task,
language-server, RPC, log and avatar boundaries. These are repository checks;
branch protection must separately require the workflow if merges must be blocked.

For quick local feedback, `CGO_ENABLED=0 go test ./...` still works. It may skip
ripgrep tests if rg is missing and skips real-gopls tests unless `SEE_GOPLS` is
set. Use the verification script before shipping.

## What the checks establish

| Boundary | Behavior checked | Evidence owner |
| --- | --- | --- |
| Native view model | Keyboard ownership, commands, typed filters, navigation, split tabs and unsaved-close confirmation | [App harness](../internal/app/harness_test.go), [group behavior](../internal/app/group_actions_test.go), [bulk close](../internal/app/tab_actions_test.go) |
| Persistence | Conflicting recovered text survives Save; newer edits survive an in-flight Save As; restored splits share buffers and retain positions | [Persistence regressions](../internal/app/persistence_behavior_test.go), [config save failure](../internal/config/config_test.go) |
| Code navigation | Actual gopls definition position and return position through Unicode; stale file-symbol replies rejected after their own callback completes | [Real gopls](../internal/app/real_gopls_test.go), [file symbols](../internal/app/file_symbols_test.go) |
| Diagnostic ownership | Published diagnostics track document version, close, server replacement and shutdown | [Server publications](../internal/langserver/diagnostics_test.go), [Problems UI](../internal/app/problems_test.go) |
| Search | Exact file-query result sets, path/name matching and byte highlights; failed/truncated subprocess output does not become a successful file list | [File queries](../internal/search/files_test.go), [process failures](../internal/search/process_test.go), [Quick Open](../internal/app/quick_paths_test.go) |
| Process lifetime | Fixed argv/cwd, failure reporting, bounded output and cancellation of a child that has explicitly reported readiness | [Tasks](../internal/tasks/tasks_test.go), [Tasks UI](../internal/app/tasks_test.go) |
| Protocol | Framing, cancellation, crashes, position conversion, symbol result shapes and request-ID-specific responses | [LSP protocol](../internal/lsp/lsp_test.go), [document symbols](../internal/lsp/document_symbols_test.go) |
| Assets and packaging | Icon precedence, both appearance assets, ZIP/tar extraction, checksum rejection and release-note generation | [Icons](../internal/app/fileicons_test.go), [tool installation](../internal/app/tools_test.go), [release notes](../scripts/changelog/test_changelog.py) |
| Other contracts | Real temporary Git repos, file classification, URI/path conversion, bounded logs, themes, avatars and dependency direction | Tests in the owning `internal` packages; [architecture policy](../internal/arch/arch_test.go) |

Architecture checks enforce repository policy; they do not substitute for
behavior tests. Asset parsing catches broken embedded resources, so these checks
remain useful even though they are not interaction tests.

## Effectiveness audit

The audit in [issue #27](https://github.com/wibus-wee/see/issues/27) prioritized
lost edits, obsolete asynchronous results, process leaks and false-success
responses. It did not target a coverage percentage. No existing test was proven
redundant enough to delete.

Temporary production faults were applied individually, checked for the expected
behavioral failure, and restored before the final run:

| Temporary fault | Check that rejected it |
| --- | --- |
| Overwrite an externally changed file without checking conflict | Recovered edits / external disk contents |
| Mark newer edits saved after writing an older snapshot | In-flight Save As keeps the buffer dirty |
| Drop restored cursor positions | Split-session exact positions |
| Accept a stale symbol reply | Request-specific completion barrier and stale-result assertion |
| Discard a definition's column | Real-gopls exact Unicode byte position |
| Truncate committed settings before a failed save | Save failure preserves old settings and retry works |
| Remove diagnostic version filtering | Publication sequence excludes obsolete diagnostics |
| Kill only the parent process | Ready child must close its live connection after cancellation |
| Duplicate a file-query result while omitting another | Exact expected path set |
| Prefer a short extension over a compound extension | Distinct synthetic icon identities |
| Answer configuration with method-not-found | Exact response payload associated with RPC ID |
| Return incorrect bytes for a ZIP download | Extracted binary content |

The file-discovery error test also failed against the original implementation:
`ListFiles` discarded scanner and process errors. The fix returns those errors
and leaves a subsequent successful retry usable.

The audit replaced two unreliable observations: a global UI callback count that
could advance for unrelated work, and a fixed sleep followed by checking that a
child had not written a file. Tests now wait for the relevant completion or
child readiness; timeouts only bound a hang.

## Limits and follow-up

The MyGo harness drives rendered native widgets without an OS window. It does
not prove desktop dialogs, real filesystem watchers, shell installation,
single-instance IPC, updater installation or real terminal rendering. These
still need native smoke checks on the target OS. The real-gopls test covers Go;
TypeScript/C++ server integration is not established by the fake-client tests.
Windows shell fixtures and privileged symlink/permission cases may skip; inspect
verbose test output when investigating a platform issue.

The harness still leaves some asynchronous workspace/timer cleanup to process
exit. If shuffled tests expose late callbacks, repair resource ownership before
adding retries. The existing recent-folder hook test checks notification
presence rather than exact notification semantics; strengthen it when that
contract changes.

For a new regression, identify the wrong user-visible result first, choose the
closest public or consumer entry point, and make the test fail for that fault.
Use controlled completion signals for asynchronous work. Keep a small pure
boundary test when it catches malformed inputs more directly than a full UI
scenario; do not repeat the same implementation assertion at several layers.
