# Lane D 复验报告（SPEC-008，窄范围复验 + 专项裁决）

复验者：独立复验工程师（非实现者、非首轮验证者）。复验对象：分支 `spec-lane-d-io` 修复后状态，commit `4a6e9c8`（修复区间 `dac68cf..4a6e9c8`，5 个提交）。
基线：首轮验证报告（`verify-lane-d` 分支 docs/specs/conformance/verify-lane-d.md，commit `d0f2f48`）判定的 needs-fix 清单（D1 必修，D2/D3/D4 建议，REQ-IO-050 证据升级）。
方法：全部证据为本人亲自构造的实验输出。为隔离变量建立两个对照 worktree：**buggy**（4a6e9c8 且 `internal/app/hex_preview.go`、`internal/hexview/search.go` 回退到 `dac68cf` 的未完成本）与 **fixed**（4a6e9c8 原样）。本人新增测试文件 `internal/app/reverify_lane_d_test.go` 与 `internal/hexview/reverify_lane_d_test.go`（随本报告一并提交），并在两个 worktree 中各放一份首轮验证者的原始对抗测试做对照运行。

## 总体判定：**merge-ready**

D1–D4 全部 confirmed-fixed，REQ-IO-050 行为证据补齐并真实，回归测试具备正负对照判别力（本人重跑：buggy 5.08s FAIL / fixed 0.08s PASS，与修复者报告一致）。全量测试唯一失败 `TestNewFileCacheFailureKeepsWelcome` 确认为既有 flake（证据见末节），非 Lane D 回归。

## 专项裁决：wedge 机制谁对谁错

**裁决：修复者的机制分析成立，首轮验证者的复现场景与机制叙述错误，但其判定的缺陷（D1）本身真实存在、严重度评估（必修）依然正确。三方各自对错分明，逐条如下。**

### 事实层（本人实证，双方无争议后可核对的部分）

`loadDoc`（app.go:1107-1131）的逻辑是：`needBuf := d.buf == nil`，`checkStamp := !needBuf || d.video`。stamp 快捷路径只在**已有 buffer 的文本文件**或**视频**上适用；快捷路径命中时后台 goroutine 提前返回，永远不执行完成处的 `d.hex = nil`（app.go，只在完整重读路径执行）。TooLarge 文档没有 editor buffer（`d.buf == nil`）也不是视频，所以 `checkStamp == false`——**任何**刷新（哪怕文件未变）都走完整重读路径，完成时无条件 `d.hex = nil`，hex 状态整体销毁重建。

### 实验 A：TooLarge 文件在 buggy 代码下自愈——验证者的"永久卡死"对该场景不成立

`TestReverifyTooLargeRefreshSelfHeals`（本人编写）：48MiB+64KiB 稀疏文件 hex 视图，跳转到第二页触发在途页读，读完成到达前 `refreshAll()`（文件未变）。buggy worktree 实测：

```
--- PASS: TestReverifyTooLargeRefreshSelfHeals (0.08s)
    reverify_lane_d_test.go:89: self-healed: the reload reset the hex state and the second window arrived; orphaned state pending map[65536:true]
```

用户可见行为：reload 完成 → `d.hex = nil` → 下一帧重新 pin → 第二页窗口到达，占位符消失。**self-heal 成立**。但注意日志细节：被遗弃的旧 hexState 的 `pending[65536]` 在 buggy 代码里**永远残留**（fixed 代码下同位置日志为 `pending map[]`）——ordering 缺陷真实存在，只是对该文档类被状态整体重置掩盖，用户不可见。

### 实验 B：文本文件在 buggy 代码下永久 wedge——修复者声称的真实场景成立

`TestReverifyTextPageHealsAfterRefresh`（本人独立编写，与修复者测试同场景不同断言路径）：100KiB 文本文件（editor 持有 buffer → stamp 快捷路径适用），同样的"跳转第二页 + 在途读 + refreshAll（文件未变）"。实测：

```
buggy:  --- FAIL: TestReverifyTextPageHealsAfterRefresh (10.03s)
            reverify_lane_d_test.go:133: wedged: the second window never resolves, a permanent placeholder: pending map[65536:true]
fixed:  --- PASS: TestReverifyTextPageHealsAfterRefresh (0.07s)
```

buggy 下 10 秒（远超任何调度延迟）内 `d.hex` 从不被重置（测试显式断言 `d.hex == hexBefore`，即 stamp 快捷路径确实命中、状态确实存活），`pending` 从不清除，页永久占位。fixed 下 0.07s 恢复。**永久 wedge 对 stamp 快捷路径适用的文档真实存在**——用户路径完全可达：任意文本文件标签页点 hex 开关（tabbar.go:682-693，`showBytes` 对文本文件为 true）后刷新（`refreshAll` 由导航栏 Refresh、git 操作后刷新等触发）。

### 裁决一：验证者复现机制错误，修复者正确

首轮验证报告 D1 节声称自己的复现（big.bin，TooLarge）"因文件未变，reload 走 `loadDoc` 的 `checkStamp` 快捷路径"——**事实错误**：big.bin 无 buffer，`checkStamp` 为 false，reload 走完整路径，`d.hex` 必被重置。验证者描述的"文件真变化时 wedge 可被治愈、未变化时无任何路径重置"对其**自己使用的文档类**恰好说反：TooLarge 文件无论变与不变都在每次 reload 完成时重置。修复者的修正（buffered 文本/视频才是永久 wedge 场景）经实验 A/B 双向证实。

但验证者的核心工程判断仍然成立，且证据强度足够：(1) ordering 缺陷本身（`delete(h.pending, offset)` 在 stale 守卫之后）经代码核实为真，实验 A 的 orphan pending 残留是其直接物证；(2) 必修定性正确——存在一个用户可达的文档类永久卡死，验证者只是错误地以为自己的复现已证明它；(3) 修复方向正确（"作废的完成也应清理 pending，或 stamp 快捷路径重置 d.hex"——实现者采用了前者）。

### 裁决二：验证者原测试在 buggy 代码上的失败到底是什么 + "line 73 在 fixed 代码上 racy"的判定

验证者原测试（`verify-lane-d` 分支 `internal/app/adversarial_verify_test.go` 的 `TestAdversarialHexPageSurvivesRefreshOfUnchangedFile`）在 buggy 代码上 10/10 失败于 line 73（`wedged: 1 pending windows never resolve`，0.02-0.07s）。这个失败在修复者框架下的真实解释是**在途读观察 + 测试自身结构缺陷**，而非用户级永久卡死：

1. 测试在 `refreshAll()` 后立即用**非阻塞** `drain()` 排水，随后 `h.tt.Frame()`，再立刻断言 `len(d.hex.pending) == 0`。此时页读完成多半尚未 post（在途观察），或刚被处理；
2. **更致命的是结构问题**：fixed 代码里完成到达即清 pending，但紧随其后的 Frame 会因为该页仍未缓存而**重新请求它**——`pending[65536]` 重新置 true。且页读完成被处理时 `d.seq` 必然已加一（`refreshAll` 在 UI 线程同步执行 `d.seq++`，先于任何 drain），完成**必被 stale 守卫丢弃**，页永远不会缓存，于是每一帧都重请求，`pending` 在 line 73 处恒为 true。

实证：同一原始测试文件在 **fixed** worktree 上 10/10 以**完全相同的方式**失败（`adversarial_verify_test.go:73: wedged: 1 pending windows never resolve ... map[65536:true]`）。一个在被修复代码上必然失败的测试不具备任何判别力。修复者"line 73 检查在修复代码上是 racy 的"的表述**方向正确但偏轻**：按上面第 2 条的结构分析，该测试在 fixed 代码上不是偶发失败而是**原则上不可能通过**（完成必被丢弃 → 页必不缓存 → 帧必重请求 → pending 必非空；若 re-pin 抢先到 line 73 之前则撞 line 70 的 Fatal——两条出路都是失败）。它是"在途观察 + frame 重请求"双重缺陷。首轮报告"修复后该测试应通过"的验收标准按其原文**不可能达成**，修复者重写场景是必要的而非规避。

附带结论：验证者的 app 层对抗测试文件**不应原样并入主线**；其 rangeio/hexview 两个对抗测试文件则健康（fixed 上全部 PASS，本人在复验中重跑确认）。

### 裁决三：修复者的回归测试判别力——成立

`TestHexPageSurvivesRefreshOfUnchangedFile` 正负对照本人重跑：

```
buggy:  --- FAIL: TestHexPageSurvivesRefreshOfUnchangedFile (5.08s)
            streamio_behavior_test.go:228: wedged: the second window never resolves after a refresh of the unchanged file: pending map[65536:true]
fixed:  --- PASS: TestHexPageSurvivesRefreshOfUnchangedFile (0.08s)
```

与修复者声称的 5.08s FAIL / 0.08s PASS 完全一致（buggy 负对照耗时恰为 5s deadline 加收尾）。判别的本质：fixed 下完成清 pending → 下一帧重请求 → 页到达，循环在毫秒内收敛；buggy 下 pending 永不清除、页永不到达，deadline 必然耗尽。修复者测试的场景选择（文本文件）是正确的场景——这一点本身也是裁决一的组成部分。测试不含对实现细节字面量的镜像断言，断言的是用户可观察结果（页到达/占位符消失）。

## 逐项复验

| 项 | 判定 | 本人证据 |
| --- | --- | --- |
| D1（必修）hex 页孔 wedge | **confirmed-fixed** | 实验 B 正负对照（10.03s wedge FAIL vs 0.07s PASS）；修复者回归测试正负对照（5.08s/0.08s）；代码核实 `delete(h.pending, offset)` 现位于一切守卫之前，对遗弃状态 delete 无害（该状态已不可达）。修复本身不改变 TooLarge 的自愈路径（实验 A 在 fixed 上同样 PASS，orphan pending 清空）。 |
| D2 markdownHeadingLine stat 前置 | **confirmed-fixed** | `TestReverifyMarkdownHeadingLineStatBound`（本人编写，分配量物证）：fixed 上 PASS——49MiB 稀疏目标返回 -1 且 `TotalAlloc` 增量 < 1MiB；buggy（回退 document_links.go）对照 FAIL：`the over-cap target was read before being dropped: 51394416 bytes allocated`——负对照证明测试判别、正对照证明修复后不再全量读入。行为保持：within-cap 小 markdown 仍正确定位 `# Install` 于 line 0。读后再检查保留（防 stat 后增长），合理。 |
| D3 Find 负 start 拒绝 | **confirmed-fixed** | fixed 上 `TestReverifyFindRejectsNegativeStart`（本人编写）PASS：空 pattern 与非空 pattern 的负 start 均报错；空 pattern 在合法 start 仍匹配该位置，ErrNotFound 语义不变。buggy 对照 FAIL：`a negative start answered -5, no error, for pattern ""`——精确复现验证者原观察。修复者加在 `TestFindAcrossChunks` 内的负 start 断言 buggy 上同步 FAIL（`empty pattern matched at a negative start`），fixed 上 PASS。app 层不可达性（`findHex` 拒空 pattern、start ≥ 0）代码核实属实，纯 API 契约修复的定位准确。 |
| D4 洞口描述修正 | **confirmed-fixed** | rangeio.go 包注释现泛指"any replacement that preserves both"并点名 rename-of-crafted-copy（rsync -t、tar 解包）与 mtime 粒度依赖；矩阵 REQ-IO-060 缺口注与"已知差距"第 2 条同步改写。描述与实证一致：本人重跑验证者 rangeio 对抗测试文件（fixed），`TestAdversarialReplaceViaRename`/`TestAdversarialWriteInPlacePreservingMtimeUndetectable` 等 7 项全部 PASS——洞口仍真实存在且描述不再窄于实际。顺带核实：`Find` 文档的内存声明已修正为"one chunk plus the pattern"（验证者 REQ-IO-030 观察项），措辞不再夸张。 |
| REQ-IO-050 证据升级 | **confirmed-fixed** | `TestHexCopyPutsTheSelectedBytesOnTheClipboard` 真实存在且是行为测试：harness 真实稀疏文件 → `selectByte` 选中 0..3 → `h.tt.Click(locales.MsgCopyHex())` 真实点击 Copy hex 按钮（走完整按钮 → `copyHex` → 后台 ReadAt → post 链路）→ 断言 Tester 剪贴板恰为 `"48 45 41 44"`（HEAD 四字节的大写 hex、单次、有序）。验证者指出的 REQ-VF-020 违规（仅 doc comment 证据）已消解；矩阵该行证据列已更新为此测试。fixed 上 PASS（0.02s）。 |

## 回归

- `CGO_ENABLED=0 go build ./...` 通过；`go vet` 对 rangeio/hexview/app/search/workspace 无输出。
- `CGO_ENABLED=0 go test ./... -count=1`（在 4a6e9c8 + 本人复验测试文件上）：除下述 flake 外全部 ok，含 hexview 0.899s、rangeio 4.911s；app 包 223s 内唯一失败为 `TestNewFileCacheFailureKeepsWelcome`。修复者声明的"全量绿"在排除该 flake 后成立。
- race：`go test -race -count=3 ./internal/rangeio/ ./internal/hexview/` 全过；`go test -race` 下 app 包 hex/copy/reverify 全部相关测试（TestHex*/TestReverify*/TestTooLargeFileReadsByWindow/TestHexViewReanchors）通过，无数据竞争报告。
- 验证者的 rangeio、hexview 对抗测试文件在 fixed 上重跑全部 PASS（hexview 含 straddle 全相位扫描 8 项 PASS，D3 修复未破坏 tail 携带逻辑）。

## 既有 flake 确认：TestNewFileCacheFailureKeepsWelcome（非 Lane D 回归，供集成参考）

- 现象：full-suite 中 `unlinkat .../Library/Caches/See/localhistory: directory not empty`（welcome_workflow_test.go:626，`os.RemoveAll` 与并发目录写入竞争）。
- 机制（代码核实）：该测试把共享 cache 目录 `~/Library/Caches/See`（测试内为隔离 HOME）整体删除并替换为**文件**；同一测试二进制内先前测试打开的文档经 `captureLoadedLocalHistory` 触发防抖写（local_history_capture.go:92 `time.AfterFunc` + :128 `go func` 后台写 `See/localhistory`），迟到写入与 RemoveAll 交错即产生此错。测试间共享文件系统状态的隔离竞争，概率性。
- 非回归证据：(1) 该测试与其涉及的 localhistory/welcome 代码均引入于 `8d281f2`/`2245222` 等 Lane D 之前的提交，Lane D 全量 diff（bbccc32..4a6e9c8）**不触碰** local_history、welcome、localhistory 任何文件（逐文件核对）；(2) 修复者报告基线同样复现；(3) 本人在 dac68cf 基线与 4a6e9c8 各跑多轮 full app suite 对照（见下）。
- 采样（`CGO_ENABLED=0 go test ./internal/app/ -count=1` 全量，每轮约 4 分钟）：**dac68cf 基线 3 轮中 2 轮复现**完全相同签名的失败（`unlinkat ...See/localhistory: directory not empty`，run1/run2 FAIL、run3 ok）；4a6e9c8 干净套件（不含验证者缺陷测试文件）首轮即复现。两侧签名逐字节一致，发生率同量级——既有 flake 确认，非 Lane D 引入。单次全绿运行（如基线首轮曾 ok）只是该随机竞争的静默面，不能作为无 flake 证据。
- 建议（超出本 lane 范围）：该 flake 需要在集成层面单独治理（测试改用私有 cache 根或串行化），但不应阻塞 Lane D 合并。

## 复验范围声明

窄范围复验：本报告只覆盖首轮 needs-fix 清单与裁决事项；首轮已 confirmed 的 REQ-IO-010..090 各条未重新全量对抗（rangeio/hexview 层借重跑验证者对抗测试文件顺带再确认，未见漂移）。两个对照 worktree 的 buggy 构造只回退了 D1/D2/D3 涉及的三个实现文件，负对照有效性逐条对应。
