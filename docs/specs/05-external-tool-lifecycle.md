# SPEC-005：外部工具与服务生命周期

## 1. 目标能力

系统能安全地发现、启动、配置、同步、重启、关闭外部工具或服务。外部工具可以是语言服务器、搜索进程、版本控制命令、远程服务代理或本地 daemon。生命周期管理必须处理崩溃、挂起、版本同步、并发调用、取消和资源泄漏。

## 2. 设计意图

- 外部进程/服务不是普通函数调用：它们会崩溃、阻塞、输出 malformed data、泄露子进程、遵守不同协议。
- 生命周期状态必须与单个请求成败分离。
- 文档/资源版本同步是正确性的一部分，不是 UI 细节。
- 用户取消、服务重启和进程退出不能导致 pending request 永久悬挂。
- 外部工具二进制路径和环境属于安全边界。

## 3. 核心定义

- **Tool descriptor**：名称、适用资源、可执行发现方式、启动命令、环境、能力探测方式、信任要求。
- **Lifecycle owner**：唯一负责 start/stop/restart 的组件。
- **Session**：一次成功启动到关闭/失败之间的实例。
- **Document/resource sync**：把本地资源状态按协议顺序同步给外部工具。
- **Pending request**：已发出但尚未收到响应/取消确认的请求。
- **Restart budget**：限制连续崩溃导致的重启频率。

## 4. Normative Requirements

### REQ-EXT-010：每个外部工具实例 MUST 有显式状态机

至少包含：`off/starting/ready/degraded/failed/stopping` 或等价状态。请求结果不得直接覆盖生命周期状态；一次请求失败不一定意味着实例 failed。

### REQ-EXT-020：启动失败 MUST 可诊断

启动失败 MUST 区分至少：executable not found、permission denied、trust/policy denied、invalid configuration、protocol handshake failure、process exited、timeout。

### REQ-EXT-030：外部工具发现 MUST 遵守信任边界

系统 MUST NOT 在不受信任的工作区/用户输入目录中隐式执行二进制，除非用户明确信任。PATH、工作区内 executable、用户配置路径、下载安装路径的优先级 MUST 明确。

### REQ-EXT-040：协议握手与能力发现 MUST 在进入 ready 前完成或明确延迟

如果某项能力必须在 ready 前可用，握手失败不得进入 ready。若能力异步发现，状态必须保持 unknown/degraded，不得假装支持。

### REQ-EXT-050：资源/文档同步 MUST 保序且带版本

若协议要求同步资源内容，系统 MUST 保证：

- open/change/close 顺序；
- 版本单调或按协议要求递增；
- 重启后重新同步必要资源；
- 过期响应不得覆盖新状态。

不得依赖“发送顺序大概正确”。

### REQ-EXT-060：实例退出 MUST 使 pending requests 终态化

进程退出、连接断开或协议失败时，所有未完成请求 MUST 收到 failed/cancelled/restarting 等终态，不得悬挂。若自动重启，旧 session 的 response 不得交付给新 session 调用方。

### REQ-EXT-070：重启 MUST 有预算和退避

连续崩溃不得造成无限 tight loop。系统 MUST 有最大重启次数、稳定运行重置窗口、指数或固定退避之一，并暴露最终 failed 状态。

### REQ-EXT-080：取消 MUST 传递到外部边界或明确不可取消

调用方取消请求时，系统 SHOULD 发送协议取消或终止进程/命令；若无法取消，必须让调用方请求进入终态，并在后台结果到达时丢弃。

### REQ-EXT-090：输出和资源 MUST 有上限

stdout/stderr/log buffer、消息大小、并发请求数、队列长度 MUST 有上限或背压策略。上限触发行为必须明确，不得无声丢数据后仍声称完整。

### REQ-EXT-100：关闭 MUST 先尝试协议关闭，再强制终止

正常关闭 SHOULD 按协议 shutdown/exit；超时后再 kill/terminate。系统 MUST 管理子进程树或说明无法管理时的平台限制。

### REQ-EXT-110：环境变量和命令行 MUST 脱敏记录

诊断可以记录 executable、参数摘要、exit code、stderr 摘要，但 MUST 脱敏 secret/token/private env。

## 5. 推荐状态机

```text
Off
  -> Starting(launch attempt)
       -> Handshaking
            -> Ready(session id, capability snapshot)
            -> Failed(startup reason)
       -> Failed(spawn/trust/config/timeout)
Ready
  -> Degraded(protocol/sync failure, still running)
  -> Restarting(reason, old session invalidated)
  -> Stopping(user/config shutdown)
  -> Failed(unrecoverable)
Stopping
  -> Off
```

session id 或 epoch 必须用于隔离响应：只有当前 session 的事件可以影响当前状态。

## 6. 文档/资源同步序列

```text
local open(v1)
  -> send open(v1)
local change(v2)
  -> queue until open acknowledged if protocol requires
  -> send change(v2)
server restart
  -> invalidate old pending
  -> start new session
  -> replay open/current content, not stale edit history
```

关键不变量：

- 外部工具看到的内容版本不得倒退；
- close 后不得继续发送该资源的 change；
- replay 只恢复当前状态，不 blindly replay 已取消的历史操作；
- stale response 用 session/version 识别并丢弃。

## 7. 边界条件

- server 在 initialize 后、第一次请求前崩溃；
- server 支持 hover 但不支持 definition；
- server 发送 malformed frame；
- server 输出超大日志导致管道阻塞；
- 用户连续快速编辑，取消旧请求；
- 进程不响应 shutdown；
- executable 在运行中被替换/删除；
- workspace untrusted 但用户配置指向 workspace binary；
- 多个资源同一服务器共享一个 session。

## 8. 本质机制 vs 实现选择

### 本质必须保留

- lifecycle state machine；
- trust-aware discovery；
- 版本化资源同步；
- pending request 终态化；
- restart budget/backoff；
- session/epoch 隔离；
- output/resource bound；
- graceful then forceful shutdown。

### 实现选择

- 用 subprocess、daemon、socket、RPC 或库嵌入；
- 是否自动安装工具；
- 单一进程还是每资源一个进程；
- 协议细节；
- 退避参数。

## 9. 行为验收场景

1. 正常启动并完成握手：进入 ready，能力快照正确。
2. 可执行文件不存在：failed，错误为 not found，不反复重启。
3. server 运行中崩溃：pending requests 全部终态化；按预算重启；新 session 重新同步当前文档。
4. 不支持某可选能力：服务仍 ready，该功能 unsupported。
5. 连续快速编辑并取消：旧请求不悬挂，过期响应不覆盖新结果。
6. shutdown 不响应：超时后强制终止，无孤儿进程。
7. malformed/oversized message：连接进入 degraded/failed 或按协议关闭，日志有界。
8. untrusted workspace binary：不会被执行，提示 trust/policy denied。

## 10. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-EXT-060 | kill server during pending hover | 请求立即/有界失败，重启后旧响应不出现 |
| REQ-EXT-050 | 编辑 v1->v3 后重启 | 新 session 收到当前 v3 内容，不回退到 v1 |
| REQ-EXT-070 | 注入连续崩溃 | 达到预算后 failed，重启间隔可观察 |
| REQ-EXT-030 | binary 位于 untrusted workspace | 未执行，状态/提示说明 trust denied |
