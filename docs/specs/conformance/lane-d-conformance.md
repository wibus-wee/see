# Lane D Conformance：流式与范围数据访问（SPEC-008）

验证基线：SPEC-008 normative requirements（docs/specs/08-streaming-range-io.md），SPEC-000 证据规则（docs/specs/00-verification-framework.md）。对照参考：Apache OpenDAL 的 range-read 语义（`core/src/types/operator`、Reader/BlockingReader 的 range/read_at、BufferStream），提取的是语义本质（range 语义、版本一致性、内存有界），未照搬其网络流模型。

## 架构说明（导航用）

- `internal/rangeio`：Lane D 新建包。`Session` 以 `Stamp{Size, ModTime}` pin 住文件版本（本地文件无 ETag，size+mtime 是唯一可靠信号）；`ReadAt` 实现 OpenDAL 式越界语义（越界非错误、short read + EOF 标志）；`Scan` 流式逐块交付、复用缓冲区、取消即停。进程级并发上限 `concurrency = 8`（`Limiter`），上限内每次读持有至多一个 fd，总内存 ≤ 8 × 调用方窗口。
- `internal/hexview`：纯格式化 + 搜索。`Find` 经 `Session.Scan` 流式扫描，跨 chunk 边界携带 tail，无全量加载。
- `internal/app/hex_preview.go` / `hex_controls.go`：hex 视图按 64KiB 页懒加载、LRU 缓存（页数上限）、并发 pending 上限；大文件（`TooLarge`，> 48MiB）经标签页 hex 开关可达。读途中文件变化 → `ErrChanged` → 整个视图丢弃重 pin，绝不混版本。复制经 `ReadAt` 单次连续读。页读的 pending 标记在完成到达时无条件清理（先于一切 stale 守卫）：作废的完成也必须释放页孔，否则未变文件的刷新（`loadDoc` 的 stamp 快捷路径不重置 hex 状态）会使该页永远卡在占位符——验证发现的 D1 缺陷，`TestHexPageSurvivesRefreshOfUnchangedFile` 为回归测试。
- `internal/app/quickopen.go` + `internal/search`：文件列表超限（200000）时 `ListFiles` 返回 `truncated` 标记，面板显示本地化截断提示（`msgOnlyTheFirstCountFilesAreListed`，en + zh-Hans），包括 "No Matches" 场景（否则该文案会撒谎）。

## 需求追踪矩阵

| Req ID | Interpretation | User/system flow | Behavioral evidence | Status | Gap |
| --- | --- | --- | --- | --- | --- |
| REQ-IO-010 | 调用方必须能区分完整加载 / 范围读取 / 流式扫描 | 打开 > 48MiB 文件 → 编辑器拒绝全载（TooLarge 提示）→ 标签页 hex 开关按 64KiB 页读取；编辑器内的完整加载保持原有 48MiB 上限语义 | `TestTooLargeFileReadsByWindow`（harness，真实 48MiB+64KiB 稀疏文件，真实 UI）：仅读 1 个 64KiB 页，页内容是文件头 | implemented | — |
| REQ-IO-020 | 定义 offset 单位（字节）、EOF/空范围/越界/负偏移/版本契约 | `Session.ReadAt` doc comment 为契约文本 | `TestReadAtRangeContract`（off==size、off>size、length 0、短读+eof）、`TestReadAtEmptyFile`、负偏移返回 error | implemented | — |
| REQ-IO-030 | 内存有界：页大小上限 + 并发上限 + 拉取式扫描 | hex 页缓存 LRU 有页数上限；Scan 是 pull-based（yield false 即停），上游不预读 | `TestScanCoversInOrder` 早停不报错、`TestScanAwaitsOneSlot`（排队而非并发膨胀）；benchmark `BenchmarkReadSparseFileWindow`（8GiB 稀疏文件） | implemented | LRU 驱逐本身无独立行为测试（属实现细节调节，非外部行为） |
| REQ-IO-040 | 取消停止上游读取并释放资源 | hex 搜索中途修改 query / 换文件 / 关闭标签 → `control.cancel()` | `TestScanCancellation`：取消后无后续 chunk、limiter 槽位不泄漏（一槽 limiter 后续扫描不挂起）；取消语义文档化（进行中的单次读取不中断，但受 length 约束有界） | implemented | — |
| REQ-IO-050 | 分块→连续内存的复制成本可见 | hex 视图复制字节：`copyHex` 一次 `ReadAt` 连续读入剪贴板；Scan 的 chunk 文档注明复用不可保留 | `TestHexCopyPutsTheSelectedBytesOnTheClipboard`（harness：真实稀疏文件 hex 视图内用 Copy hex 按钮复制选中字节，剪贴板收到且仅收到该选区的 hex）+ 代码契约（单次连续读，无隐藏拷贝路径） | implemented | — |
| REQ-IO-060 | 多次 range read 保证同一版本，否则明确失败 | hex 视图所有读取经同一 `Session`；文件 mtime/size 变化 → `ErrChanged` → 视图整体丢弃重 pin | `TestHexViewReanchorsToTheChangedVersion`（harness：真实修改文件后，旧版本读取失败 → invalidate → 重 pin 后的 stamp 是新版本、读到新内容）、`TestReadAtFileChanged`、`TestScanFileChangedMidway`、`TestFindReportsTheFileChanging`（2GiB 稀疏文件扫描途中变化）、`TestHexPageSurvivesRefreshOfUnchangedFile`（未变文件的刷新使在途页读作废后页孔自愈的回归测试） | implemented | 任何保持 size+mtime 完全一致的替换不可检测——不仅原位写，rename 替换（rsync -t、tar 解包等正常工具流）同样如此；粗 mtime 粒度文件系统上连不刻意的快速重写都可能漏检（本地文件系统普遍限制，已在 rangeio 包注释写明） |
| REQ-IO-070 | 短读与 EOF 可区分 | `ReadAt` 返回 `(data, eof, err)`；hex 页尾页以 `PageLen` 截短 | `TestReadAtRangeContract` 短读返回实际长度 + eof=true；不补零 | implemented | — |
| REQ-IO-080 | 大输出截断必须可见，不得静默 | Open Quickly 文件列表超 200000 → `truncated` 标记 → 面板显示"仅列出前 200000 个文件"（有中英文案） | `TestQuickOpenShowsTheListingIsPartial`（harness：truncated 时提示可见、不截断时不可见）；`search_test.go` rg 与 walk 两路径 limit 行为 | implemented（本 lane 范围） | **跨 lane**：任务/终端输出上限属 Lane A `internal/tasks`，未动；搜索**结果**输出的截断展示同理 |
| REQ-IO-090 | 并发读取有资源治理，超限行为明确 | 多标签页多 hex 视图并发 | `concurrency = 8` 进程级 Limiter；超限**排队**（acquire 阻塞可取消），`TestScanAwaitsOneSlot` 验证排队而非失败或并发膨胀；fd 随用随关 | implemented | — |

## 行为验收场景对照（SPEC-008 §10）

| 场景 | 证据 | 结论 |
| --- | --- | --- |
| 1. 巨型资源读小范围，成本与范围相关 | `TestReadWindowCostIndependentOfFileSize`：1MiB vs 8GiB 稀疏文件读同一 64KiB 窗口，分配次数差 ≤ 1、耗时同量级（5 次取最小值抗调度噪声） | ✅ |
| 2. 下游暂停 → 上游停止/有界 | `TestScanCoversInOrder` 早停；Scan pull-based 无预读 | ✅ |
| 3. 读取中取消有界释放 | `TestScanCancellation`：无后续 chunk + 槽位不泄漏 | ✅ |
| 4. 越界 range 契约 | `TestReadAtRangeContract`：off≥size → 空+eof；length 0 → 无 I/O；负偏移 → error | ✅ |
| 5. 读取期间文件变化 | harness `TestHexViewReanchorsToTheChangedVersion` 完整用户路径 + 三个单元测试 | ✅ |
| 6. 大输出截断可见 | quick open 场景已实现并本地化；进程输出截断为 Lane A 领域（见 REQ-IO-080 跨 lane 备注） | ⚠️ 部分（跨 lane） |
| 7. 连续内存只复制一次 | copyHex 单次 ReadAt；无重复拷贝路径 | ✅ |
| 8. 并发多大文件资源限制 | Limiter 排队语义 + `TestScanAwaitsOneSlot` | ✅ |

## OpenDAL 对照结论（用户要求的参考学习）

| OpenDAL 语义 | 来源 | 在 See 的对应 |
| --- | --- | --- |
| `read_with().range(a..b)` 越界非错误，返回短读 | operator range 语义 | `ReadAt` short read + eof 标志 |
| 版本一致性由 ETag/conditional request 保证 | Reader 契约 | 本地无快照 → `Stamp{Size, ModTime}` pin + 每次读取前后验证，失败即 `ErrChanged` |
| `ConcurrentTasks` 有界并发 + prefetch 上限、drop 即取消 | `raw/futures_util.rs` | `Limiter` 并发上限 8 + acquire 可取消；hex 视图 pending 页数上限 |
| `BufferStream` 状态机：EOF 后不再产出、错误终结流 | `types/read/buffer_stream.rs` | Scan 的版本验证先于每次 yield，变化即终结，绝不交付半版本数据 |
| 非连续缓冲 vs linearize 成本 | opendal Buffer 抽象 | Scan chunk 复用（文档禁止 retain）；copy 路径单次显式连续读 |

## 既有实现核查（gap 分析起点，非 Lane D 引入）

- `workspace.ReadFile` 有 48MiB 上限且先 stat、拒绝非常规文件——完整加载路径本身已合规（有界、拒绝静默）。
- 图片预览 `io.LimitReader(MaxSize+1)` 有界；视频经播放器流式打开，无全量加载。
- `applyEditsToFile` 全量读写是重写文本编辑的本质需求，不适用 range 语义。
- `markdownHeadingLine`（document_links.go）原为无界 `os.ReadFile`，本 lane 加了 48MiB 上限且 stat 前置——超出即放弃锚点跳转，不把超限文件读入内存（验证修复 D2）。

## 已知差距

1. **跨 lane**：进程/终端输出的截断展示归 Lane A 的 `internal/tasks`，本 lane 未触碰。若该处截断不可见，REQ-IO-080 的场景 6 在系统级仍是 partial。
2. 任何保持 size+mtime 不变的替换都无法检测版本变化——原位写入只是其一，rename 替换（rsync -t、tar 解包一类真实工具流）同样不可检测；检测还依赖文件系统 mtime 粒度（粗粒度系统上快速重写可能落在同一 tick 内）——所有本地文件系统的共同限制，已在包文档写明。
3. hex 搜索/复制在大文件上的取消是协作式的：进行中的单次磁盘读取不中断（有界于窗口大小），文档已注明。
4. LRU 页驱逐未做独立行为测试（内存调节实现细节，非外部可观察行为）。
