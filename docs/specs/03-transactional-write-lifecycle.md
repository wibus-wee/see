# SPEC-003：事务式写入生命周期

## 1. 目标能力

系统对文件、对象、文档或配置等持久资源的写入具备明确的 begin/write/close(commit)/abort 语义。调用方能知道写入是否完整提交；失败、取消或崩溃不会留下被误认为成功的半成品；并发冲突和条件写入有可验证行为。

## 2. 设计意图

- 把“写了一些字节”与“逻辑写入完成”分开。
- 防止进程崩溃、磁盘错误、取消或部分失败后留下截断文件。
- 让错误后的重试不会重复追加或覆盖错误版本。
- 让 close/flush/commit 成为显式状态转换，而不是依赖析构、垃圾回收或调用者习惯。
- 明确临时文件、备份、锁和冲突处理的所有权。

## 3. 核心定义

- **Write session**：一次逻辑写入，从创建到 commit/abort/close failure 结束。
- **Buffering**：提交前暂存数据，可在内存或临时存储。
- **Commit**：使本次写入以完整逻辑单元对外可见。
- **Abort**：放弃写入并清理未提交状态。
- **Publish**：把 staging 内容原子或近似原子地变成目标内容。
- **Condition**：写入前置条件，例如目标不存在、版本匹配、内容哈希匹配。
- **Crash window**：从第一个副作用到 commit 完成之间可能崩溃的窗口。

## 4. Normative Requirements

### REQ-WR-010：写入 MUST 有显式完成点

调用方必须能区分：

1. 数据已被接受进 write session；
2. 数据已持久化或至少进入提交协议；
3. 数据已作为完整结果对外可见。

仅返回“写入了 N 字节”不得暗示逻辑保存成功。

### REQ-WR-020：每次输入接受 MUST 有明确原子性粒度

系统 MUST 写明：单个 write call 是全部接受、全部拒绝，还是可能部分接受。如果可能部分接受，返回值和后续状态 MUST 能表达已接受字节/记录数。

### REQ-WR-030：close/commit 失败 MUST NOT 假装成功

如果 flush、fsync、rename、网络确认或最终校验失败，close MUST 返回失败或不确定状态。析构/drop/defer 中的清理错误不得掩盖已返回的错误。

### REQ-WR-040：失败后 write session MUST 进入受控终态或恢复态

发生终端失败后，session 不得继续接受数据并假装仍是同一逻辑写入。系统 MUST 选择并文档化：

- poisoned：所有后续 write/close 失败；
- recoverable：可重试 close；
- abort-required：必须 abort，或实现自动 abort。

### REQ-WR-050：取消/abort MUST 有清理语义

abort 后不得把未提交内容作为目标结果暴露。临时文件、multipart upload、锁或事务资源 MUST 被清理，或明确保留用于调用方恢复并在文档中说明所有权。

### REQ-WR-060：对覆盖式本地文件写入 SHOULD 使用 staging + atomic publish

推荐顺序：

1. 在目标同目录或同文件系统创建临时文件；
2. 写完整内容；
3. flush/close，必要时 fsync；
4. 校验；
5. rename/compare-and-swap/publish 到目标；
6. 清理失败 staging。

直接截断目标文件再写入 MAY 用于明确允许非原子语义的接口，但不得冒充安全保存。

### REQ-WR-070：条件写入 MUST 在 publish 边界执行

`if-not-exists`、版本匹配、哈希匹配等条件 MUST 尽量在最终原子 publish 中判断。先检查再写入存在 TOCTOU 风险时，必须用锁、原子 rename、条件请求或其他机制消除；否则不得声称支持条件语义。

### REQ-WR-080：临时错误与永久错误 MUST 影响 staging 处理

如果错误可能只是临时 publish 失败，且重试 close/publish 是安全的，系统 SHOULD 保留 staging 供重试。永久错误 MUST 清理或隔离 staging。实现 MUST 防止 staging 被其他 session 误用。

### REQ-WR-090：并发写 MUST 有明确冲突结果

同一目标的并发 write session 必须满足以下之一：

- 明确 last-writer-wins，并文档化可能覆盖；
- 使用锁/版本条件，冲突方收到 conflict；
- 创建多个版本并暴露版本 identity。

不得静默交错写入产生混合内容。

### REQ-WR-100：崩溃恢复 MUST 可验证

系统 MUST 定义崩溃后临时文件/未完成事务的可见性、清理时间和恢复方式。用户不得看到半提交内容被当作正式内容。

### REQ-WR-110：写入语义 MUST 覆盖元数据

权限、修改时间、BOM、换行、扩展属性或对象元数据若属于目标契约，MUST 在 commit 中正确保留或更新；不能因 staging 替换而意外丢失。无法保留时必须在能力或错误中表达。

## 5. 状态机

```text
Idle
  -> Open(session, target, options)
  -> Writing(buffered/accepted bytes)
  -> Closing
       -> Committing
            -> Committed
            -> CommitRetryable(staging retained)
            -> FailedPermanent(staging cleaned/quarantined)
            -> Uncertain(reconciliation required)
       -> Aborting
            -> Aborted
  -> Poisoned (terminal failure)
```

关键规则：

- `Committed` 后才能向调用方报告保存成功。
- `CommitRetryable` 只允许按同一 session identity 重试 publish，不得重新追加已接受数据。
- `FailedPermanent` 和 `Aborted` 后 session 不再接受输入。
- `Uncertain` 要求调用方读取/校验目标，不得自动盲目重试非幂等写。

## 6. 关键不变量

1. 目标内容要么保持旧版本，要么呈现完整新版本，除非接口明确声明非原子。
2. 同一次输入不会被 commit 两次。
3. staging 不会在成功后继续覆盖目标。
4. 一个 session 的临时资源只由该 session 或恢复流程清理。
5. 错误发生后，外部可观察状态与返回给调用方的状态一致。

## 7. 边界条件

- 磁盘满：commit 失败，目标旧内容保持；staging 清理或保留按错误分类。
- 权限不足：尽早失败；不得在目标目录留下无所有权临时文件。
- 目标被外部修改：若启用冲突检测，必须 conflict；否则明确 last-writer-wins。
- 目标是 symlink：必须定义跟随、替换 symlink、还是拒绝；不能因实现巧合改变语义。
- 跨文件系统临时目录：rename 不再原子，必须改用同目录 staging 或声明降级。
- 大文件写入中取消：已写 staging 可清理；不得暴露部分目标。
- close 时进程崩溃：重启后不得把 staging 当目标；恢复流程可清理。

## 8. 本质机制 vs 实现选择

### 本质必须保留

- 显式 commit/abort；
- 失败后的受控状态；
- staging/publish 或等价原子性保证；
- 条件写入在正确边界执行；
- 冲突与崩溃恢复语义；
- 不把部分写入当成功。

### 实现选择

- 临时文件命名；
- 是否 fsync 目录；
- 使用 rename、hard link、对象 multipart、数据库事务还是 CAS；
- 是否暴露 Writer 对象或只暴露 save API；
- 是否在后台异步 commit。

## 9. 行为验收场景

1. 正常保存后目标字节完全一致，权限/BOM/换行符合契约。
2. 写入中途注入磁盘错误：旧目标保持或明确失败，无半个新文件。
3. close/rename 注入临时错误：按契约可重试 publish；成功后只提交一次。
4. 永久错误：staging 被清理或隔离，session poisoned。
5. 并发两人保存同一目标：结果符合 last-writer-wins/conflict/versioning 契约。
6. if-not-exists：已存在目标不被覆盖，返回 condition failure。
7. 写大文件中途取消：目标仍为上版本，staging 处理符合文档。
8. 崩溃模拟：重启后临时文件不冒充正式文件，恢复/清理行为符合文档。

## 10. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-WR-030 | 注入 rename 失败后保存 | UI/API 报告失败，不提示保存成功；目标旧内容不变 |
| REQ-WR-060 | 正常保存期间观察目录 | 先出现 staging，publish 后 staging 消失；无直接截断窗口 |
| REQ-WR-090 | 并发保存两个版本 | 最终内容与契约一致，不出现两版本字节交错 |
