# 独立复验 — Lane B / SPEC-006 修复轮(reverify-lane-b)

- 复验者:独立复验 session,分支 `reverify-lane-b`(基于修复后 tip `5773664`)
- 复验对象:`spec-lane-b-paths` 修复轮 4 个 commit:`559aaf2`(F1 full-fold)、`bca45db`(F3 尾随点/空格拒绝)、`605215a`(F2 session 恢复 canonicalize)、`5773664`(矩阵更正)
- 依据:`verify-lane-b.md`(commit `78058e6`)的 must-fix 清单;范围限于确认修复真实有效且无回归,未重做全量对抗验证
- 环境:macOS darwin/arm64,Go 1.27.4,APFS(merging)

## 总体判定:**merge-ready**

三条必修项全部 confirmed-fixed,文档裁定如实落地,修复者声称的 25 包全绿由复验者独立跑出同样结果。未修改 Lane B 任何实现代码;复验者自写的探针测试均在运行后删除,未入库。

## 逐条裁定

### F1 — fold 表(full case folding):**confirmed-fixed**

- 实现核实:`internal/workspace/paths.go` `foldKey` 确实改用 `cases.Fold`(golang.org/x/text,已在依赖中)对 NFC 结果做全折叠,不是只加测试没改实现。`cases.Fold()` 无语言参数,行为是全量 Unicode case folding。
- `TestAdversarialFullFoldDivergence` PASS,日志输出 `fs merges İ / i+dot-above: true; FoldEqual: true`(修复前为 `FoldEqual: false` + divergence)。
- 复验者追加独立探针(验证者扫描之外的字符组合,真实 APFS 卷上实测):U+00DF `ß`↔`ss`、U+1E9E `ẞ`↔`ss`、希腊 sigma `σ`↔`ς`、U+0130 `İ`↔`i+U+0307` —— 四组全部 `fsMerges=true foldEqual=true converge=true`,即文件系统合并处身份收敛,且收敛到磁盘真实拼写。探针运行后删除。
- 反向安全性不变:fold 只是候选关系,`os.SameFile`/目录枚举探测仍是裁决者,候选网放宽不产生误合并。

### F2 — markdown preview tab 恢复:**confirmed-fixed**

- `TestAdversarialMarkdownPreviewRestoreSpelling` PASS,日志输出 `stale-key present=false canonical-key present=true` + `no split`;`TestAdversarialRegularTabRestoreConverges` 同样 PASS。
- 实现核实(审查 `605215a` 的 `session.go` diff):恢复路径对文件类 tab 统一经 `DiskSpelling`,untitled 草稿保留合成路径(正确——磁盘上无对应拼写);Active/Preview 键走同一归一化,`markdownTab` 前缀键先解前缀再归一。
- 误伤审查:
  - **diff tab 恢复**:diff tab 走 `restoreDiffTab` 提前分支,不进入路径归一化;Active/Preview 键有 `isDiffTab` 显式豁免,按存储原样匹配。既有测试 `TestAutoDiscoveryPreservesRestoredContainingRepositoryGitTab`(restored diff tab + Active 键匹配)在全量测试中 PASS。
  - **untitled 恢复**:`tab.Untitled` 分支跳过 `DiskSpelling`,`validDraftPath` 校验在归一化后的同一变量上进行,键一致;welcome workflow 的 untitled/恢复测试全量 PASS。
  - `standaloneSessionDocs`/`pendingStandaloneRecovery` 仍按存储拼写 `tab.Path` 查询,与恢复记录写入时的拼写键一致(recovery 记录本身按写入时的 doc.path 键控,见下),不构成新缝隙。
  - `openIn` 对已 canonical 的路径再次 canonicalize 是幂等no-op(`DiskSpelling` 输入已干净绝对路径,逐组件探测得到自身),代价仅是一次多余目录枚举,正确性无碍。

### F3 — 尾随点/空格组件拒绝:**confirmed-fixed**

- `TestAdversarialTrailingDotComponents` PASS(已按拒绝语义重写,断言 `.. /x.txt`、`notes.`、`file `、`dir /file.txt`、`...`、` . ` 全拒,`a.b/file.txt`、`my file.txt`、`.hidden`、`sub/.hidden/x.txt` 正常 join)。
- 全平台统一拒绝已落地为**词法拒绝而非平台分支**:`cleanRel` 中对每个组件做 `strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ")` 判断,无任何 `runtime.GOOS` 条件(文件中唯一的平台分支是既有的 Windows UNC 输入拒绝,与此无关)。卷命名空间的论据(SMB/exFAT 卷的 POSIX 客户端共享 strip 语义)作为保守拒绝的理由在注释和矩阵中如实记录;即便对该论据的严格性存疑(NTFS 本身可经 `\\?\` 存此类名,strip 是 Win32 客户端规范化),结论是超集拒绝,方向安全。
- 不误伤核实(复验者独立探针,验证者清单之外的名字):`.hidden`、`a.b/file.txt`、`my file.txt`、` .leading-space-ok`、`file.name.with.dots.txt`、`a b/c d.txt`、`..hidden` 全部正常 join;`.. /evil`、`x/.`、`a/.. `、`evil. ` 拒绝或正确消解(`x/.` → `x` 是 `.` 组件消解的既有文档化语义,非尾随点案例)。`...` 被拒绝属文档化的 POSIX 超集拒绝,与修复意图一致。探针运行后删除。
- 行为入口覆盖:`TestEntryPromptConfinedToItsFolder` 增补 `.. /evil-entry.txt` 与 `notes.` 输入,全量 PASS。

### 文档裁定:**confirmed-fixed**

对照 `5773664` 的矩阵 diff 逐条核实:

- D1 机制描述已更正:`openInMode` 无条件 canonicalize、普通标签页恢复一直收敛、真正漏洞是 preview tab 恢复分支——与复验者代码审查结论一致;D1 标记为已修复,REQ-PATH-080 行重写并保留 D2 为唯一残留。
- F4(case-only rename + 旧 gone tab 静默覆盖)与 F5(symlink dual-identity 过期视图无标记)均写入"已知缺口"节,机制、后果、实证测试名、非回归定性(VS Code 同类)如实记录。
- REQ-PATH-040 行记录了全平台词法拒绝、POSIX 超集拒绝的代价、Windows 行为未实机执行的诚实限定;REQ-PATH-060 行记录了 29 字符发散的发现与修复。
- 未发现文档与实现不符之处。

## 回归检查(全部复验者亲自执行)

| 检查 | 结果 |
| --- | --- |
| `CGO_ENABLED=0 go build ./...` | 干净,exit 0 |
| `CGO_ENABLED=0 go test -count=1 ./...` | **25 包全 PASS**(internal/app 223.4s),与修复者声称一致 |
| `go test -count=1 -race ./internal/workspace/` | PASS,零数据竞争 |
| `go test -count=1 -race ./internal/app/` | PASS(417.2s),零数据竞争 |
| `go vet ./internal/workspace/ ./internal/app/` | 干净 |

## 对抗一击:大小写重命名后会话内 recovery 记录键缝隙 —— **确认预存,非回归**

场景:打开 `A.txt` 并有未保存编辑(recovery 记录按 `A.txt` 键写入)→ app 内大小写重命名为 `a.txt` → `renamed()` 先 `removeRecovery`(删除旧键记录)再 `cacheRecovery`(异步写新键记录)→ 两操作之间进程崩溃,或新记录尚未落盘时崩溃,未保存编辑的恢复记录丢失。

核实:`rename.go` 的 remove-then-recache 序列在修复轮基线 `2fe8b60` 已存在(逐行比对一致),`git diff 2fe8b60 HEAD -- internal/app/rename.go internal/app/recovery.go` 为空——修复轮未触碰该路径,`renamed()` 本就以新拼写续身份,与大小写重命名无特殊交互。此缝隙是所有 rename 共有的崩溃窗口(大小写重命名只是其中一例),属预存的 contract 缝隙而非本次修复引入的回归。与矩阵"已知缺口"节的 F4 并列记录即可,不阻塞合并。

## 残留问题(均不阻塞合并)

1. 上述 rename 崩溃窗口(预存)。
2. `DiskSpelling` 无 fast path 的注释更正已并入 `559aaf2`(原验证报告 hygiene 节指出),network FS 上每次 open 逐祖先目录枚举的代价现已如实记录,无行动项。
3. Windows 实机行为仍未验证——尾随点/空格现已全平台词法拒绝,该缺口的实际暴露面已消除,此项降为纯文档限定。
