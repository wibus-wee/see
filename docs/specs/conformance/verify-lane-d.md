# Lane D 独立验证报告（SPEC-008）

验证者：独立验证工程师（非实现者）。验证对象：分支 `spec-lane-d-io`，commit `dac68cf`。
验证基线：SPEC-008（docs/specs/08-streaming-range-io.md）、SPEC-000 证据规则、实现者的 conformance 矩阵（docs/specs/conformance/lane-d-conformance.md）。
方法：重跑实现者的全部测试；自己编写并运行 14 个对抗性测试（下文逐条标注）；对矩阵中每条 REQ 的关键声明构造实现者未覆盖的场景实测。所有"证据"均为本验证者在自己的 worktree 内亲自复现的输出，不引用实现者的测试结论。

## 总体判定：**needs-fix**

1 个必修缺陷（hex 视图页孔永久卡死，D1），2 个建议修复的低严重度缺陷（D2、D3），若干观察项。rangeio 包本体的契约、版本 pin、取消与并发治理经对抗验证全部成立；问题集中在 app 层 hex 视图生命周期与个别边角。

## 逐条 REQ 判定

| Req | 判定 | 本验证者证据 |
| --- | --- | --- |
| REQ-IO-010 | **confirmed** | 亲自重跑 `TestTooLargeFileReadsByWindow`（PASS，0.19s）：48MiB+64KiB 稀疏文件 kind=TooLarge、hex 视图仅读 1 个 64KiB 页、页内容为文件头、无预读。`BenchmarkReadSparseFileWindow`：8GiB 稀疏文件 64KiB 窗口读 18205 ns/op、66168 B/op、6 allocs/op——分配与耗时与窗口大小而非文件大小相关。`TestReadWindowCostIndependentOfFileSize` 连跑 3 次 PASS。语义区分真实存在：编辑器路径 `workspace.ReadFile` 先 stat 后读、超 48MiB 不分配（workspace.go:207-214，与矩阵描述一致）。 |
| REQ-IO-020 | **confirmed** | 亲自重跑 `TestReadAtRangeContract`/`TestReadAtEmptyFile`（PASS）；自写 `TestAdversarialReadAtBoundaries`（PASS）补验精确边界：恰好读到末尾 eof=true、差一字节 eof=false、length 0 无 I/O。负偏移/负长度均报错（实现者测试+自验）。offset 单位字节、[offset, off+len) 区间语义在 doc comment 中明确。 |
| REQ-IO-030 | **confirmed**（一处观察项） | pull-based Scan 无预读、yield false 即停（`TestScanCoversInOrder` PASS）。hex 页 LRU 8 页 × 64KiB、pending 上限 8，per-view 内存有界（代码核实）。观察项：`hexview.Find` 文档声称"memory stays at one chunk"不准确——tail 拼接时每 chunk 重新分配 window，`BenchmarkFindInSparseFile` 实测扫 1GiB 累计分配 1.1GB/op 垃圾（live memory 仍有界，属 GC 压力，非内存超限）；pattern 长度 ≥ chunk 时窗口内存确实超一个 chunk。两者都不违反 REQ（有界成立），但文档措辞夸张。 |
| REQ-IO-040 | **confirmed** | 亲自重跑 `TestScanCancellation`（PASS）：取消后 yielded==1 无后续 chunk、单槽 limiter 后续扫描 10s 内完成（槽位不泄漏）。`TestScanAwaitsOneSlot`（PASS）：排队扫描可被取消、返回 ctx 错误。实现者的声明"进行中的单次读取不中断但有界"经代码核实为真，且已在 doc comment 写明。Goroutine 无泄漏路径（全部 defer）。 |
| REQ-IO-050 | **confirmed**（证据等级按 SPEC-000 从严应标 partial） | 矩阵的证据是"代码契约（doc comment）+ 无隐藏拷贝路径"，这**不是行为证据**——按 REQ-VF-020"仅有单元测试或类型检查不得标记 implemented"，矩阵此条标 implemented 属过度声明。但本验证者独立核查了实质：`copyHex` 对选中区间单次 `ReadAt` 连续读、一次性 hex 编码入剪贴板，无重复拷贝路径；app 内未发现 copyHex 的行为测试（`grep copyHex internal/app/*_test.go` 无结果），也无剪贴板测试替身。实质正确、行为证据缺失，故本人判定 confirmed（实质）同时记录矩阵标注违规。 |
| REQ-IO-060 | **confirmed**（文档局限声明属实，但比文档写的更宽） | 亲自重跑 `TestHexViewReanchorsToTheChangedVersion`/`TestReadAtFileChanged`/`TestScanFileChangedMidway`/`TestFindReportsTheFileChanging`（全部 PASS，`-race` 下亦过）。自写对抗测试：`TestAdversarialScanTruncateSmallerMidway`/`TestAdversarialScanGrowMidway`（PASS，truncate 变小与增长中途均被 ErrChanged 终结）；`TestAdversarialWriteInPlaceWithoutMtimeDetected`（PASS，APFS 上 mtime 纳秒级，普通原位写入可检测——实测 20 次快速重写产生 20 个不同 mtime）。**关键验证**：矩阵承认的"size+mtime 不变则不可检测"局限属实——`TestAdversarialWriteInPlacePreservingMtimeUndetectable` 与 `TestAdversarialReplaceViaRename` 均复现：不仅原位写，**rename 替换**（rsync -t / tar 解包一类真实工具流）只要保持 size+mtime 同样不可检测，此时 ReadAt 返回的是 session 未 pin 住版本的字节。文档原文"writes in place that preserve both"仅覆盖前者，实际洞口更宽。这是本地文件系统固有限制，文档化要求本身满足（REQ-IO-060 只要求"明确"），但描述不准确，见 D4。重 pin 无版本混合：ErrChanged 完成处理先 `d.hex = nil` 丢弃整个状态（含已缓存页），迟到完成由 `d.hex != h` 守卫丢弃（代码核实 + harness 测试 PASS）。 |
| REQ-IO-070 | **confirmed** | `TestReadAtRangeContract` 短读返回实际长度 20 + eof=true、不补零（亲自重跑 PASS）；`TestAdversarialReadAtBoundaries` 补验精确 eof 边界。`readFullAt` 对定位读合法的短读做循环补偿（代码核实）。 |
| REQ-IO-080 | **confirmed**（证据为分层组合，规模外推） | 亲自重跑 `TestQuickOpenShowsTheListingIsPartial`（PASS）：truncated 时提示可见、不截断时不可见。`internal/search` 的 rg 与 walk 两路径 limit 单元测试 PASS。代码核实完整链路：`ListFiles(limit=200000)` → `ensureFiles` 赋值 `q.truncated` → 面板在命令/符号/行号模式之外显示 `msgOnlyTheFirstCountFilesAreListed`；en 文案 "Only the first {count} files are listed — type more of the name."，zh-Hans "仅列出前 {count} 个文件——请继续输入名称。"，catalog_gen.go 模板替换正确。No-Matches 场景下提示仍显示（quickopen.go:66,110 在 switch 之外渲染）——矩阵声称的"否则该文案会撒谎"语义成立。列表错误与空结果分离：`case q.err != nil` 在 `len==0` 之前。限制：200000 文件真实规模端到端测试本验证者尝试过但不可行——APFS 上单目录 2 万个硬链接需 45 秒（目录项插入超线性），无法在合理时间构造 200k 文件工作区；截断常量接线由代码核实背书。跨 lane 部分（任务输出截断）矩阵已诚实标注不在本 lane。 |
| REQ-IO-090 | **confirmed** | `TestScanAwaitsOneSlot`（PASS）：超限排队而非失败、等待可取消、槽位释放后排队者运行。`concurrency = 8` 进程级 Limiter；fd 随用随关（每次操作 open/close，无跨调用持有，代码核实；Session 文档明确"holds no resources between calls"）。实测内存上界成立：in-flight ≤ 8 × 调用方窗口。注意项：一个正在进行的 Find 扫描独占一个槽位扫完整个文件，8 个并发大文件扫描可饿死全部 hex 页读（排队语义下合法，UX 代价，非违反）。 |

SPEC-008 §10 行为验收场景 1-5、7、8 经上述证据覆盖成立；场景 6 的进程输出部分为跨 lane 事项，矩阵已声明。

## 缺陷列表

### D1【必修，严重度：中】hex 视图页孔永久卡死：对未变文件的刷新使在途页读作废且 pending 永不清理

**复现**（确定性，`internal/app/adversarial_verify_test.go` 中 `TestAdversarialHexPageSurvivesRefreshOfUnchangedFile`，本验证者编写）：

1. 打开 >48MiB 文件，切到 hex 视图；
2. 跳转/滚动触发一个尚未缓存的页（offset 65536）请求，页读 goroutine 在途；
3. 在读完成到达 UI 线程前，点导航栏 Refresh（或任何触发 `refreshAll`/`reloadDoc` 的路径：git 操作后刷新、watch 事件等）——文件**没有任何变化**；
4. `loadDoc` 起始即 `d.seq++`，在途页读完成时被 `d.seq != seq` 守卫丢弃——丢弃发生在 `delete(h.pending, offset)` **之前**（hex_preview.go:222-229）；
5. 因文件未变，reload 走 `loadDoc` 的 `checkStamp` 快捷路径（app.go:1121-1130），stat 一致即提前返回，**根本不经过**完成处的 `d.hex = nil`（app.go:1196-1200）——该行只在完整重读路径执行。因此二进制/TooLarge 文件（`d.buf == nil`）文件真变化时会经完整路径重置 `d.hex`（wedge 可被 ErrChanged/重 pin 治愈），而文件**未变化**时无任何路径重置，`pending` 永远卡住。

实测输出：

```
--- FAIL: TestAdversarialHexPageSurvivesRefreshOfUnchangedFile (0.02s)
    adversarial_verify_test.go:73: wedged: 1 pending windows never resolve after a refresh of the unchanged file: map[65536:true]
```

**实际行为**：`pending[65536]` 永远为 true。用户滚动到该页永远看到 `…` 占位符，`requestHexPage` 因 `h.pending[offset]` 拒绝重新请求。8 个这样的孔填满 pending 上限后**整个 hex 视图的页加载完全冻结**。恢复途径只有关闭标签页或文件真实变化（ErrChanged 路径）。

**期望行为**：作废的完成也应清理 pending（`delete` 移到守卫之前），或 stamp 快捷路径重置 `d.hex`。

**归责说明**：此模式（先 guard 后 delete pending）在 Lane D 之前的旧实现中已存在（`git show bbccc32:internal/app/hex_preview.go` 同样结构），属既有缺陷；但 Lane D 重写了该文件的读取路径并把 hex 视图数据流纳入自己的 conformance 矩阵（REQ-IO-030/060/090 的行为主体），缺陷存活在 lane 声称合规的表面上，故计入本次验证结论。

### D2【建议修复，严重度：低】`markdownHeadingLine` 的 48MiB 上限不约束内存

`internal/app/document_links.go:377-380`：`os.ReadFile(path)` **先全量读入内存**，然后才检查 `len(data) > workspace.MaxSize` 丢弃。矩阵（"既有实现核查"节）声称"本 lane 加了 48MiB 上限（超出即放弃锚点跳转，不阻塞 UI）"——上限只约束处理不约束内存：一个 2GiB markdown 链接目标会被完整读入再丢弃。正确做法是 stat 前置（`workspace.ReadFile` 自身就是这么做的）。属 REQ-IO-030 实质缺口但影响面小（一次性、随后释放）。

### D3【建议修复，严重度：极低】`hexview.Find` 空 pattern + 负 start 返回文件外位置

`Find(ctx, s, -5, nil)` 返回 `( -5, nil )`：空 pattern 分支只检查 `start <= s.Size()` 不检查下界。实测输出：`empty pattern at start -5: off=-5 err=<nil>`。app 层不可达（`findHex` 拒绝空 pattern 且 start 恒 ≥ 0），纯 API 契约瑕疵，与 `Scan`/`ReadAt` 拒绝负值的契约不一致。

### D4【文档修正，严重度：极低】不可检测洞口的描述窄于实际

rangeio 包注释与矩阵称"writes in place that preserve both [size and mtime] are not detectable"。实测（`TestAdversarialReplaceViaRename`）：**rename 替换**保持 size+mtime 同样不可检测——这不需要任何"原位写入"，是 rsync -t、tar 解包等正常工具流的真实行为。建议措辞改为泛指"任何保持 size 与 mtime 的替换"。另：文档未提 mtime 粒度维度——本验证者实测 APFS 为纳秒级（20 次快速重写 20 个 distinct mtime），粗粒度文件系统（FAT 2 秒）上连不刻意的快速重写都可能漏检。

## 观察项（非缺陷）

1. `hexview.Find` 扫描分配的 GC 垃圾 ≈ 文件大小（见 REQ-IO-030），live memory 有界。若大文件搜索成为热点，可复用拼接缓冲。
2. 搜索扫描期间独占一个 Limiter 槽位，扫描不可中断粒度为 256KiB chunk（有界，文档已注明）。
3. `TestQuickOpenShowsTheListingIsPartial` 是合成状态测试（手工设 `truncated`），非端到端；真实规模端到端不可行（见 REQ-IO-080），但分层证据链完整，接受。
4. tab 切换不取消进行中搜索、tab 关闭正确取消（groups.go:654 核实）。
5. `ensureFiles` 在 `err != nil` 时 `truncated` 与文件列表置空一致，无陈旧标记问题。

## 工程卫生

- `CGO_ENABLED=0 go build ./...` 通过；`go vet` 对 lane 涉及包无输出。
- 全量测试 `CGO_ENABLED=0 go test ./... -count=1`：25 个包 ok，**唯一失败是本验证者自己加的 D1 复现测试**；即实现者声明的"全量测试绿"在加入对抗测试前成立。
- `-race`：`./internal/rangeio ./internal/hexview`（count=3）、app 包 4 个 harness 测试 + 对抗测试全部通过，无数据竞争报告。
- 时序敏感测试（`TestReadWindowCostIndependentOfFileSize`、`TestScanCancellation`、`TestScanAwaitsOneSlot`）连跑 3 次稳定；`TestFindReportsTheFileChanging` 连跑 5 次稳定。
- commit `dac68cf` 单提交、conventional 风格与仓库一致（对照 `feat(diff):`、`fix(i18n):` 等历史）；改动文件全部在 lane 范围内，无无关夹带（`en.json` 同时修复一处行尾缺失换行的 JSON 格式问题，顺带但无害）。
- 实现者的 harness 测试质量不错：`TestTooLargeFileReadsByWindow`/`TestHexViewReanchorsToTheChangedVersion` 走真实 UI 帧 + 真实稀疏文件 + 真实文件修改，非 proxy；断言针对行为（页内容、页数、stamp 更替）非实现细节字面量。`rangeio` 单测覆盖真实文件系统语义。未发现镜像实现细节或照抄字面量的断言。

## 本验证者新增的测试文件（均标注 Adversarial，非实现者编写）

- `internal/rangeio/adversarial_verify_test.go`：7 个测试，全部 PASS（其中 2 个"确认洞口存在"的测试以 PASS 形式确认局限声明属实）。
- `internal/hexview/adversarial_verify_test.go`：4 个测试，全部 PASS（含 D3 的确认记录）；`TestAdversarialFindEveryStraddlePosition` 对 chunk 边界 straddle 全相位扫描，验证 tail 携带逻辑无遗漏无双报。
- `internal/app/adversarial_verify_test.go`：2 个测试；`TestAdversarialHexPageSurvivesRefreshOfUnchangedFile` **FAIL——即 D1 的确定性复现**；`TestAdversarialHexSearchCanceledByRefresh` PASS（搜索路径因 control 随 seq 重置可恢复，与页缓存路径形成对照）。

## 必修清单（merge-ready 前置条件）

1. **D1**：修复 pending 清理顺序（`delete(h.pending, offset)` 必须在 stale 守卫 return 之前执行）或等价方案；修复后 `TestAdversarialHexPageSurvivesRefreshOfUnchangedFile` 应通过。
2. **D4**（可同修）：更正 rangeio 包注释与矩阵中洞口描述，泛指保持 size+mtime 的任何替换（含 rename），并提一句 mtime 粒度依赖。

建议但非必修：D2（stat 前置）、D3（空 pattern 下界检查）、REQ-IO-050 矩阵证据降级为 partial 或补 copyHex 行为测试、Find 的扫描分配优化。
