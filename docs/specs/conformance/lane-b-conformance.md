# Conformance Report — SPEC-006 资源身份与路径边界规范化（Lane B）

- Target revision: spec-lane-b-paths（base f3d214c，本 lane 提交见 git log）
- Environment: macOS（darwin/arm64, Go, APFS）。本机临时目录实测 case merge = true、Unicode normalization merge = true（探测程序输出 `case merge: true / unicode merge: true`），大小写/Unicode 用例的合并分支真实执行，非跳过。
- Specification revision: docs/specs/06-resource-identity-paths.md（与 base 同版本）

## 身份契约（解释规格后的落地语义）

实现定义并文档化于 `internal/workspace/paths.go` 包注释：

- **逻辑身份**：用户到达文件的绝对干净路径，按磁盘真实拼写（大小写/Unicode 归一化以文件系统实际行为为准，用候选目录枚举 + 存在性探测判定，不假设平台）。工作区打开的文档身份即此。
- **根是 symlink**：内部状态用用户进入的拼写（canonical root），语言服务器回报的 physical path 经 `DocPath` 映射回逻辑身份——同一文件一个 buffer。
- **root 内部 symlink**：穿过它的路径是同一字节的第二个逻辑身份，永不静默合并（`dual identity` 契约，SPEC-006 §4 REQ-PATH-050 允许三选一）。用户可观察的代价：写 target 的 watcher 事件只携带 target 拼写（真实 mygo 后端实测），symlink 身份的视图不会随之重载，永久显示过期内容且无任何标记——保存时 byte-compare conflict 是唯一兜底（无静默数据丢失，`TestAdversarialSymlinkDualIdentityStaleView`）。
- **confined join**：`JoinRel`/`ValidRel` 词法拒绝 `..` 逃逸、绝对路径、NUL、Windows device/盘符；See 不承诺物理 confinement（打开 root 外文件是合法产品特性，如拖入单文件），confinement 用于约束工具/输入文本合成的根内路径。
- **rename**：app 内 rename = 身份延续；watcher 报告的 rename = 旧身份关闭 + 不自动开新文档。

## 需求追踪矩阵

| Req ID | Status | Behavioral evidence | Gap / reproduction |
| --- | --- | --- | --- |
| REQ-PATH-010 | implemented | `internal/workspace/paths.go` 包注释定义 absolute/root-relative/display/URI 四类；display 仅出现在渲染层（`a.root` trim 前缀、`RelPath`），不作 key。行为证据：以下各用例中 buffer/diagnostics/git 键互相对上 | - |
| REQ-PATH-020 | implemented | 边界清单：用户输入 `openInMode`（tree/CLI/拖放/reopen 共用）`internal/app/app.go`；watcher `applyEvents`；diagnostics `updateProblems`；workspace edit `applyWorkspaceEdit`；quick-open/git 工具输出 `joinRoot`/`JoinRel`；新建文件输入 `submitEntry`。行为证据：`TestOpenCaseVariantSharesOneDocument`、`TestWatcherEventsAreConfinedToTheRoot` 等（见下） | - |
| REQ-PATH-030 | implemented | `ValidRel`/`JoinRel` 文档化规则：slash 输入、重复分隔符折叠、`.` 消解、空/`.`=根、尾部分隔符不保留、NUL 拒绝、尾随点/空格组件拒绝（Win32 规范化无法忠实表达，见 REQ-PATH-040 行）。证据：`internal/workspace/paths_test.go` `TestJoinRelConfines`/`TestValidRel` 表驱动含全部条款 | - |
| REQ-PATH-040 | implemented | `root/a/../../outside` 类输入被拒绝：`TestJoinRelConfines`（unit）、`TestEntryPromptConfinedToItsFolder`（行为：5 种逃逸输入全部报 err 且外部文件未创建，合法嵌套正常创建）、`TestWatcherEventsAreConfinedToTheRoot`（行为：逃逸 watcher 事件不触碰 root 外打开的文档）。不做纯字符串拼接：join 后二次 `Within` 校验。尾随点/空格组件（`.. `、`notes.`）全平台词法拒绝：`TestAdversarialTrailingDotComponents`——Win32 打开时剥离尾随点/空格，`.. ` 会规范化为 `..` 逃逸 confinement；剥离是卷命名空间语义而非客户端 OS 语义（SMB/exFAT 卷的 POSIX 客户端同样受影响），故不做平台分支。这是 POSIX 上的超集拒绝：此类名字在 POSIX 是合法字面名，但正常项目中几乎不存在，Windows 上根本无法创建；代价与依据见 `cleanRel` 注释。POSIX 合并/拒绝分支本机实测。三平台 CI 实证时发现并修复：`filepath.IsAbs` 在 Windows 对卷锚定路径（`\x`，当前盘根）返回 false，`JoinRel` 曾把 `/etc/passwd` 重植到工作区内而非拒绝——违反本契约的 volume-anchored 条款，实现已修复（拒绝非空 VolumeName 与前导分隔符）；ubuntu/windows CI 绿 | 物理 confinement 按契约不承诺（规格该条款为条件项“若系统承诺”），已文档化 |
| REQ-PATH-050 | implemented | `TestRealPathOfSymlinkedRootIsOneDocument`：symlink root 打开工作区后按 physical path 打开同一文件 → 同一 `*doc`，docs 计数 1。`TestInteriorSymlinkIsOwnLogicalIdentity`：内部 symlink 产生第二身份、两身份内容一致、各自拼写稳定 | - |
| REQ-PATH-060 | implemented | `TestOpenCaseVariantSharesOneDocument` / `TestOpenUnicodeVariantSharesOneDocument`：合并型文件系统上变体打开 → 同一 doc、activePath 为磁盘拼写；探测非合并时验证走“不同名”分支（本机实测走了合并分支）。`DiskSpelling`/`FoldEqual` 单测覆盖 NFC/NFD/大小写候选。`foldKey` 用 full case folding（`cases.Fold`），与 APFS/NTFS 一致——simple mapping（`strings.ToLower`）下土耳其 İ、德文 ẞ、希腊 prosgegrammeni 区块共 29 个字符磁盘合并但身份不收敛（独立验证发现并实证），已修复：`TestAdversarialFullFoldDivergence`、`TestFoldEqual`（全折叠断言） | 三平台 CI 实证。过程中发现并修复两个测试侧平台假设缺陷（实现语义未变）：(1) 各 fsMerges 探针在求值 os.SameFile 前删除了探针文件——POSIX 的 stat 快照是急求值的，Windows 的 SameFile 按记忆路径惰性重开文件，删除后合并型文件系统被误报为敏感型；SameFile 现在在探针存活时求值。(2) `TestAdversarialCaseRenameReopenConverges` 把合并型语义当作普遍真理；rename 后旧拼写在敏感型文件系统上无所指，必须原样返回。测试现在探测文件系统自身行为并断言测得的语义分支，与 Lane B 设计原则一致；sensitive 分支在 case-sensitive APFS 卷上实测通过 |
| REQ-PATH-070 | implemented | `internal/uri/uri_test.go`：新增真实文件 round-trip——空格、`%`、非 ASCII、Windows drive、UNC；`TestPathRoundTrips` 表驱动含 `%25` 不重解码场景。URI decode 仅 `uri.ToPath` 一处，app 内无第二处 percent 处理 | - |
| REQ-PATH-080 | partial | 文档/quick-open/git status/watcher/diagnostics/session 恢复统一以 canonical 逻辑身份为键，证据：上述全部行为测试 + `TestWorkspaceEditLandsOnTheOpenDocument`（LSP 用变体 URI 编辑落在已开 buffer 而非幻影写盘）。diagnostics 键经 `uri.ToPath`→`DiskSpelling`→`DocPath` 归一。session 恢复：`openInMode` 本就无条件 canonicalize（原矩阵 D1 的机制描述“只在 tabs 命中时才 canonicalize”经独立验证证伪，普通标签页恢复一直收敛，`TestAdversarialRegularTabRestoreConverges`）；真正的漏洞是 markdown preview tab 恢复分支绕过归一直接用存储拼写建 doc，会话间大小写重命名后重开同一文件造成 buffer 分裂，已修复（恢复时对文件类 tab 统一 `DiskSpelling`，untitled 草稿保留合成路径）：`TestAdversarialMarkdownPreviewRestoreSpelling` | 残留差距见 Deviations D2（localhistory） |
| REQ-PATH-090 | implemented | `TestInAppRenameContinuesTheDocument`：app 内 rename 后同一 `*doc` 同一 buffer，tab 跟随，旧键消失。`TestWatcherRenameClosesOneIdentityAndOpensNone`：watcher rename 后旧 doc gone、path 不变、新路径无自动文档、重新 open 得到新 doc | - |

## User-flow acceptance

1. Flow: 用户在大小写不敏感文件系统上通过 tree 打开 `a.txt`，再从 CLI/拖放/链接以 `A.TXT` 打开
   Setup: APFS 临时目录真实文件
   Steps: `userOpen` 两种拼写
   Expected: 一个 buffer，身份为磁盘拼写
   Actual: 同一 `*doc` 指针，docs==1，activePath 为磁盘拼写
   Evidence: `TestOpenCaseVariantSharesOneDocument` PASS（合并分支实测执行）
2. Flow: 语言服务器对变体 URI 发 workspace edit
   Expected: 落在已开 buffer；未打开文件则写磁盘真实文件，不产生幻影文件/第二 buffer
   Actual: buffer 文本被编辑且磁盘在保存前不变；未打开时磁盘真实文件内容正确更新
   Evidence: `TestWorkspaceEditLandsOnTheOpenDocument` / `TestWorkspaceEditOfUnopenedCanonicalFile` PASS
3. Flow: watcher 报告逃逸路径与 rename
   Expected: 逃逸不影响 root 外文档；rename 不产生重复 buffer
   Actual: 如上
   Evidence: `TestWatcherEventsAreConfinedToTheRoot` / `TestWatcherRenameClosesOneIdentityAndOpensNone` PASS
4. Flow: 新建文件 prompt 输入 `../evil-entry.txt` 等
   Expected: 拒绝且不创建任何文件
   Actual: err 非空，`os.Lstat` 外部路径不存在；合法嵌套 `sub/ok.txt` 创建成功
   Evidence: `TestEntryPromptConfinedToItsFolder` PASS

## Deviations

- D1 — **已修复**（原描述经独立验证证伪后重写，见 REQ-PATH-080 行）：markdown preview tab 恢复分支绕过 canonicalize 造成 buffer 分裂。恢复路径现在统一经 `DiskSpelling`；回归测试 `TestAdversarialMarkdownPreviewRestoreSpelling`。
- D2 — REQ-PATH-080 partial：`internal/localhistory` 内部以 `EvalSymlinks` 后的 physical canonicalPath 为键（lane 所有权外，未改动）。后果：root 内 symlink 与其 target 两个逻辑身份共享一份 local history，与 dual identity 契约在这一处缓存上不一致。
  Classification: partial。影响：低——历史内容一致（同一字节序列），不会出现错误恢复，仅历史条目归属粒度与文档身份不同。独立验证复核认为“影响低”准确。
  建议：后续由 localhistory owner 决定按逻辑身份键控或文档化豁免。
- D3 — changelog fixture 测试在本机失败：`test_release_body_uses_reviewed_notes...` 需要 GitHub release 凭据（`credentials_exhausted`），与本 lane 无关（未触碰 release 脚本；Go 全部 25 个包 PASS，`go tool mygo vet` 干净）。
  Classification: unverifiable（环境）。

## 已知缺口（契约形态的固有代价，非回归，独立验证实证）

- **case-only rename + 旧 gone tab 保存 = 静默覆盖**：外部大小写重命名后，真实 macOS 后端会报告 rename，旧标签页标记 gone，树中列出的是新拼写。若用户编辑并保存旧标签页，`saveDoc` 的 byte-compare 读旧路径——在合并型文件系统上仍解析到同一文件——比较通过，写入不可见地落进改名后的文件，而用户的另一个标签页仍显示旧内容，全程无 conflict。这是 watcher-rename 契约（旧身份关闭）与合并型文件系统交互的固有后果，不是回归；VS Code 同类。实证：`TestAdversarialExternalCaseRenameReopenSplit`、`TestAdversarialWatcherMissedCaseRenameSplits`。未来的修法方向：保存时检测拼写漂移或链接两个身份。
- **symlink dual identity 的过期视图无标记**：见上文身份契约末节。写 `real.txt` 的事件永远到不了 `link.txt` 视图，用户看到的是永久过期且无标记的内容，仅保存时 conflict 兜底。实证：`TestAdversarialSymlinkDualIdentityStaleView`（PASS，作为行为文档）。

## Coverage summary

- MUST implemented: 8/9（REQ-PATH-080 仍为 partial：D1 已修复，残留 D2 是 localhistory 所有权外的缓存粒度差距，独立验证复核认为“影响低”准确；REQ-PATH-040 的 Windows 尾随点/空格语义以全平台词法拒绝实现，POSIX 上的超集拒绝已文档化，不再是未实现的平台语义）
- SHOULD/MAY: 规格无独立 SHOULD 编号项；行为验收场景 1/2/3/5/6 已测，场景 4（Windows 分隔符混合）由 uri/workspace 表驱动单测覆盖、场景 7（display 不改键）由契约结构保证（display 仅为渲染时 trim）
- 非单测证据：所有 app 层用例经 harness 走真实用户入口（userOpen/applyEvents/applyWorkspaceEdit/submitEntry）+ 真实临时文件系统（真实 symlink、真实 Unicode/大小写变体文件名）；平台合并语义由真实探测程序确认后断言相应分支
- 独立验证（verify-lane-b）后的修复轮：F1（fold 表换成 full folding）、F2（preview tab 恢复 canonicalize）、F3（尾随点/空格组件全平台词法拒绝）均已修复，验证者的实证测试移植为回归测试——`TestAdversarialFullFoldDivergence`、`TestAdversarialMarkdownPreviewRestoreSpelling`、`TestAdversarialTrailingDotComponents`（重写为拒绝语义）；验证者其余对抗测试一并移植于 `internal/workspace/paths_adversarial_verify_test.go`、`internal/app/paths_adversarial_verify_test.go`
- verify.sh: `go test ./...` 25 包全 PASS、`go tool mygo vet` 干净；python changelog  fixtures 因凭据环境失败（D3）
