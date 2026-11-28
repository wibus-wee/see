# SPEC-008：流式与范围数据访问

## 1. 目标能力

系统能处理大于内存或大于即时加载预算的数据资源，支持范围读取、分页、流式消费、背压、取消和内存上限。调用方不必为了读取一小部分而加载完整资源；实现也不得用隐藏的大拷贝破坏可预测成本。

## 2. 设计意图

- 大文件/大对象/长输出是常态，不是异常。
- 范围读取和流式处理的成本应与请求的数据量相关，而不是与资源总大小相关。
- 非连续缓冲能减少复制，但不能把复杂性隐藏成不可预测内存峰值。
- 取消必须真正停止上游生产和资源占用。

## 3. 核心定义

- **Range read**：读取 `[offset, offset+length)` 或等价区间。
- **Paging**：按固定或可配置 page/chunk 加载。
- **Streaming**：数据边到达边消费，不要求完整驻留。
- **Backpressure**：下游慢时限制上游继续生产。
- **Buffered body**：引用计数或分块持有数据，可 cheap clone/slice。
- **Linearization**：把分块数据复制成连续内存；这是有成本操作。
- **Resource bound**：内存、打开句柄、并发 stream、队列大小上限。

## 4. Normative Requirements

### REQ-IO-010：接口 MUST 区分完整加载、范围读取和流式读取

调用方必须能明确选择语义。不得用完整加载实现范围读取却声称成本与范围大小相关，除非明确文档化这是降级。

### REQ-IO-020：范围语义 MUST 精确

系统 MUST 定义 offset 单位、是否含端点、EOF 行为、空范围、越界范围、负偏移不支持方式、并发修改时的版本/一致性。

### REQ-IO-030：内存使用 MUST 有界或背压

流式消费 MUST 提供 buffer 上限、窗口、pull-based iteration、channel backpressure 或等价机制。下游停止消费时上游不得无限读入内存。

### REQ-IO-040：取消 MUST 停止上游读取并释放资源

调用方取消后，系统 MUST 有界时间内停止网络/文件/进程读取，关闭句柄或释放引用。若后台清理异步进行，接口不得提前声称资源已释放。

### REQ-IO-050：分块缓冲 MUST 明确复制成本

如果内部用分块/非连续 buffer，调用方要求连续内存时 MUST 明确这是 linearization/copy。不得把大对象无提示复制多次。

### REQ-IO-060：数据版本一致性 MUST 明确

多次 range read 是否保证来自同一版本必须写明。若底层资源可能变化，系统应支持 generation/etag/version 或在结果中暴露无法保证一致性。拼接不同版本数据不得被当作同一逻辑对象。

### REQ-IO-070：短读和 EOF MUST 可区分

范围读取可能返回少于请求字节。接口 MUST 让调用方知道实际长度以及是否到达 EOF，不能静默补零或截断。

### REQ-IO-080：大输出 MUST 有上限、截断标记或流式交付

外部进程日志、搜索结果、文件预览等若不能完整保留，必须显示截断事实和保留策略。不得静默丢失开头/结尾后仍声称完整。

### REQ-IO-090：并发流 MUST 有资源治理

同时打开多个 stream/page 时，应限制文件句柄、连接、内存和任务数；超过限制后的排队/失败行为必须明确。

## 5. 典型交互流程

```text
open resource metadata
  -> determine size/version/capability
  -> consumer requests range or stream
  -> system validates range and capability
  -> create bounded read session
  -> consumer pulls chunks / system pushes with backpressure
  -> cancel | EOF | error
  -> release resource
  -> final status visible
```

## 6. 状态机

```text
Closed
  -> Opened(resource identity/version)
  -> Reading(position, buffered bytes)
       -> Backpressured(downstream full)
       -> Reading
  -> EOF
  -> Cancelled(cleanup pending/complete)
  -> Failed(kind/status)
  -> Closed
```

关键规则：EOF 后不再产生数据；cancelled/failed 后旧 chunk 不得继续交付；重新读取必须显式创建新 session 或定义 seek 语义。

## 7. 关键不变量

1. 单页读取成本与页大小相关，而非总资源大小（除非明确降级）。
2. 任意时刻 buffering 不超过配置上限加上下层必要窗口。
3. 同一逻辑 stream 的数据顺序稳定，不重复、不遗漏；如发生重试，必须从正确位置恢复或失败。
4. cancel 后不再占用上游资源。
5. 截断/降级必须可见。

## 8. 边界条件

- 空文件；
- offset == size；
- offset > size；
- length 为 0；
- 文件在读取期间被截短或增长；
- chunk 到达但下游已取消；
- 大文件预览只需要首屏；
- 底层不支持 seek/range；
- 压缩/编码导致字节范围与字符范围不一致；
- 二进制与非 UTF-8 文本。

## 9. 本质机制 vs 实现选择

### 本质必须保留

- range/stream/完整加载语义分离；
- backpressure/内存上限；
- cancel 传播；
- EOF/short-read 语义；
- 版本一致性；
- linearization 成本可见；
- 截断可见。

### 实现选择

- page size；
- push/pull API；
- 是否使用 mmap；
- 是否引用计数 buffer；
- 并发预取策略；
- 具体线程/async runtime。

## 10. 行为验收场景

1. 10GB 资源读取 4KiB 范围：内存/耗时与 4KiB 相关，不加载全量。
2. 下游暂停消费：上游读取停止或 buffering 不超过上限。
3. 读取中取消：文件句柄/连接/进程有界释放，无后续 chunk。
4. offset/length 越界：按契约返回 empty、EOF 或 range error。
5. 文件读取期间变化：结果按版本契约一致或明确失败/重新打开。
6. 大进程输出超过上限：显示截断标记，保留策略正确。
7. 分块数据要求连续内存：只发生一次明确 linearization，不重复复制。
8. 并发打开多个大文件：资源限制生效，行为为排队或明确失败。

## 11. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-IO-010 | 读取大文件首 64KiB | 内存峰值远低于整文件，耗时与页大小相关 |
| REQ-IO-040 | 播放/预览中取消 | 上游 fd/connection 有界关闭，日志无后续 chunk |
| REQ-IO-080 | 生成 10MB 工具输出 | UI/日志明确 truncated，未声称完整 |
