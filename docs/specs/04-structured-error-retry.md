# SPEC-004：结构化错误、重试与取消语义

## 1. 目标能力

系统把错误建模为可判断、可追踪、可安全展示的结构，而不是不透明字符串。错误能回答：什么类别失败、是否可能重试、是否已经重试过、涉及哪个操作/资源、能否向用户展示、是否包含敏感信息。取消是独立语义，不等同于普通失败。

## 2. 设计意图

- 让重试策略基于语义而非字符串匹配。
- 让 UI/调用方能给出可行动提示。
- 让错误上下文在一次边界统一补充，避免每个调用点手写格式。
- 防止 secret、token、内部路径或用户隐私意外进入日志。
- 区分 temporary、permanent、persistent，避免无限重试或误重试。

## 3. 核心定义

- **Error kind**：失败的语义类别，例如 unsupported、not-found、permission-denied、conflict、invalid-input、timeout、unexpected。
- **Retry status**：`temporary`、`permanent`、`persistent` 或等价分类。
- **Temporary**：相同请求稍后可能成功。
- **Permanent**：相同请求在无外部变化时不应重试。
- **Persistent**：已按策略重试仍未成功；不再自动重试，但未来外部条件变化后可再次尝试。
- **Cancellation**：调用方主动放弃结果；不是后端错误。
- **Context**：operation、resource identity、range/version/attempt 等结构化键值。
- **Redaction**：在日志、错误展示、遥测中移除或遮蔽敏感数据。

## 4. Normative Requirements

### REQ-ERR-010：错误 MUST 同时表达 kind 与 retry status

仅有 kind 不足以决定是否重试。例如 timeout 可能 temporary，permission denied 通常 permanent。系统 MUST 允许二者独立判断。

### REQ-ERR-020：最小 kind 集合 MUST 覆盖外部边界

至少应能表达或映射：

- unexpected/internal；
- unsupported；
- invalid input/configuration；
- not found；
- permission denied；
- already exists / conflict / condition mismatch；
- range/input not satisfiable；
- rate limited；
- checksum/content mismatch；
- timeout/deadline exceeded；
- cancelled。

具体枚举可不同，但 MUST 不丢失这些语义。

### REQ-ERR-030：重试 MUST 只作用于 temporary 且操作允许重试的错误

permanent、invalid input、unsupported、冲突类错误不得自动重试。即使 kind 是 temporary，非幂等或无法 reconcile 的操作也不得默认重试。

### REQ-ERR-040：超过重试预算后 MUST 转为 persistent 或等价终态

调用方必须能区分“第一次临时失败”和“已耗尽重试策略的失败”。persistent 不应被同一自动策略继续无界重试。

### REQ-ERR-050：错误上下文 MUST 结构化且集中补充

系统 SHOULD 在操作边界补充：operation、resource/path、service/tool、range/version、attempt、duration。业务深处 MAY 添加专有上下文，但不得依赖每个调用点手写统一格式。

### REQ-ERR-060：错误和日志 MUST 有脱敏边界

secret、token、密码、完整 authorization header、私钥、用户私有环境变量 MUST NOT 进入错误字符串、结构化 context、日志或遥测。无法确认安全的字段 SHOULD 默认不记录。

### REQ-ERR-070：取消 MUST 与普通失败区分

用户或调用方取消 MUST 产生 cancelled 或等价状态。若底层操作已产生不确定副作用，结果 MUST 标记 uncertain 或要求 reconcile，不得简单改成 success。

### REQ-ERR-080：错误链 MUST 保留机器可判断信息

可以使用错误包装、cause chain、source、code 或平台异常，但从外层错误 MUST 能判断 kind/status/operation。禁止只保留格式化后的字符串。

### REQ-ERR-090：unexpected 错误 SHOULD 携带诊断深度，普通错误不必

内部 bug 或协议破坏可采集 stack/trace/debug detail；用户输入错误、not found 等预期错误不应付出不必要成本或泄露内部实现。

### REQ-ERR-100：跨进程/网络错误映射 MUST 保守

无法识别的外部错误不得随意映射为 permanent。默认 SHOULD 是 unexpected/temporary 或按协议规范分类；只有明确证据表明重试无意义时才标 permanent。

## 5. 错误分类决策

```text
external failure observed
  -> protocol/platform says exact cause?
       yes -> map kind + default retry status
       no  -> unexpected/unknown, conservative status
  -> operation idempotent/reconcilable?
       no  -> do not auto retry; uncertain if side effect may have occurred
       yes -> retry only if temporary and budget remains
  -> budget exhausted -> persistent
  -> caller cancelled -> cancelled, then reconcile finality
```

## 6. 状态流转

```text
NoError
  -> FailedTemporary(kind, attempt)
  -> Retrying(next delay, attempt+1)
  -> FailedTemporary(kind, attempt+1)
  -> FailedPersistent(kind, total attempts)
  -> FailedPermanent(kind)
  -> Cancelled(finality)
  -> Succeeded
```

规则：

- `FailedPersistent` 与 `FailedPermanent` 都是自动重试终态，但语义不同。
- `Cancelled` 需要附带 finality：`no-effect`、`aborted`、`unknown`。
- 成功后错误状态不可变；失败终态不能被后续无关操作覆盖。

## 7. 关键不变量

1. 用户看到的错误类别与日志/遥测中的类别一致，除非展示层有意简化且不丢失诊断链接。
2. 自动重试次数有上限。
3. 错误上下文不泄露 secret。
4. 重试不会改变操作输入语义；range/版本/条件必须保持原意。
5. 取消后不能继续消耗重试预算。

## 8. 边界条件

- 连接在请求可能已到达后断开：结果不确定；非幂等操作不得自动重试。
- DNS 暂时失败：通常 temporary，但配置中主机名拼错可能 permanent；可区分时应区分。
- 429/rate limit：temporary，应尊重服务端 retry-after 或退避。
- 403/permission denied：通常 permanent；凭证刷新流程是上层新操作，不算同一自动重试。
- checksum mismatch：通常 permanent 或数据损坏；重读只在明确 range/version 一致时才可尝试。
- context cancellation 与底层 timeout 同时发生：优先真实因果，无法确定时标记 cancelled/uncertain 而非 success。

## 9. 本质机制 vs 实现选择

### 本质必须保留

- kind 与 retry status 分离；
- temporary/permanent/persistent；
- 结构化上下文；
- 取消 finality；
- 安全脱敏；
- 重试上限和幂等性判断。

### 实现选择

- 错误类型用 enum、code、class hierarchy 或 result type；
- 是否使用 stack trace；
- 上下文是 map 还是 typed fields；
- 日志格式；
- 重试退避算法。

## 10. 行为验收场景

1. 注入一次 temporary 网络错误：操作重试并成功，日志显示 attempt。
2. 连续 temporary 错误：达到预算后返回 persistent，不无限重试。
3. unsupported capability：不进入重试，直接 unsupported。
4. permission denied：不自动重试，UI/调用方提示权限问题。
5. 取消下载/搜索：状态为 cancelled，不显示底层失败；无额外尝试。
6. 非幂等写连接中断：不自动重试；报告 uncertain 或要求检查目标。
7. 错误含 token 环境：日志和错误输出中不可见 secret。
8. range/版本重试：重试仍使用原条件，不把不同版本数据拼接成同一结果。

## 11. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-ERR-030 | unsupported 操作注入日志 | 0 次自动 retry，错误为 unsupported |
| REQ-ERR-040 | chaos 100% temporary failure | 达到预算后 persistent，尝试次数正确 |
| REQ-ERR-060 | 配置含 secret 的失败命令 | 日志/错误仅显示 redacted 值 |
| REQ-ERR-070 | 用户在重试等待中取消 | 无新尝试，最终 cancelled |
