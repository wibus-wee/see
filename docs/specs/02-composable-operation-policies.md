# SPEC-002：不可变组合与操作策略管线

## 1. 目标能力

系统能够在不修改各业务适配器的情况下，为同一类操作组合超时、取消、日志、指标、并发限制、重试、故障注入、协议校验等横切策略。策略顺序明确，组成后的句柄不可变；派生配置不会影响已经开始执行的操作。

## 2. 设计意图

- 把策略从业务协议解析中分离，避免每个适配器手写一份超时/日志/重试。
- 让组合顺序成为显式语义，而不是调用链巧合。
- 让同一操作在不同上下文使用不同资源或策略。
- 支持测试时注入故障，而生产路径保持同一形状。
- 避免运行中修改共享句柄导致不可预测行为。

## 3. 核心定义

- **Operation**：一次有输入、输出、副作用或外部交互的调用，例如 read/write/search/query/execute。
- **Operation input**：本次调用特有的参数，例如路径、范围、文本、查询。
- **Runtime resource**：执行操作所需但可替换的资源，例如 executor、transport、clock、process runner、network client。
- **Policy**：围绕操作或运行时资源的横切行为，例如 timeout、logging、retry、concurrency limit。
- **Composition**：把基础实现与一组有序策略组合成最终可调用对象。
- **Derived handle**：基于原组合修改配置后得到的新句柄；原句柄和在途操作不受影响。

## 4. Normative Requirements

### REQ-POL-010：操作输入 MUST 与运行时资源分离

一次操作 MUST 能区分：

1. 调用方显式传入的业务参数；
2. 执行该操作使用的运行时资源；
3. 应用到该操作的策略。

业务实现不得因为构建时捕获了某个 executor/transport/runner，就无法被后续上下文替换。

### REQ-POL-020：组合后的句柄 SHOULD 不可变

对外共享的操作句柄 SHOULD 是 immutable value 或等效对象。添加策略或替换资源 MUST 产生派生句柄，不得 retroactively 改变：

- 已分发的克隆；
- 正在执行的操作；
- 其他并发调用方。

若目标平台无法完全不可变，MUST 至少保证配置变更与操作快照有明确并发语义。

### REQ-POL-030：策略 MUST 有确定顺序

系统 MUST 文档化策略应用顺序。顺序至少影响：

- timeout 在 retry 内：限制每次尝试；
- retry 在 timeout 内：限制总调用；
- logging 在 error enrichment 内外：日志上下文是否完整；
- concurrency limit 在 retry 外：限制总并发还是尝试并发。

默认顺序 MUST 固定，不允许因调用方注册顺序巧合产生未说明语义。

### REQ-POL-040：策略 MUST 能作用于操作边界和资源边界

系统 SHOULD 区分两类扩展点：

1. 操作包装：读写参数、结果、错误、重试、观察；
2. 资源包装：executor、transport、clock、process runner。

仅提供一种扩展点时，MUST 说明另一种需求如何实现或明确不支持。

### REQ-POL-050：资源包装 MUST 转发未被替换的资源

如果某个策略只替换 executor，它 MUST 保留已有 transport/clock/runner 等资源；反之亦然。禁止 wrapper 因构造新上下文而静默丢失下层资源。

### REQ-POL-060：策略不得改变业务协议语义

除非策略的明确职责就是模拟或降级能力，否则它 MUST NOT 修改业务数据、重新解释协议、吞掉错误或隐藏副作用。策略改变可观察能力时 MUST 参与 capability 更新（见 SPEC-001）。

### REQ-POL-070：重试和取消 MUST 有边界契约

重试策略 MUST 只对幂等或有恢复协议的操作启用。取消后必须满足以下之一：

- 操作未开始副作用；
- 操作明确中止并清理；
- 操作可安全重试；
- 操作返回不确定状态并要求调用方 reconcile。

不得在取消后把后台继续成功的结果伪装成用户已取消成功。

### REQ-POL-080：故障注入 SHOULD 复用同一组合机制

测试用的 chaos/fault injection SHOULD 作为普通策略插入，而不是要求业务实现维护测试专用分支。注入点 MUST 可控、可计数、可关闭。

### REQ-POL-090：策略配置 MUST 有验证和作用域

超时、并发数、重试次数等配置 MUST 验证非法值。全局配置、按操作配置、单次调用覆盖之间的优先级 MUST 明确。

## 5. 推荐机制，但不是强制结构

一种可行模型：

```text
base operation implementation
  -> service/operation hooks in declared order
  -> resource/context hooks in declared order
  -> immutable dispatch snapshot
```

执行时：

1. 固化本次操作快照；
2. 校验能力和参数；
3. 进入最外层策略；
4. 逐层到达业务实现；
5. 错误沿相反方向被补充上下文、分类、记录或重试。

这只是参考机制。实现者可以使用 middleware、decorator、interceptor、pipeline、monadic composition 或显式调用对象，只要满足外部语义。

## 6. 状态流转

```text
BaseConfigured
  -> Composed(snapshot)
  -> Executing(snapshot, attempt=1)
  -> RetryWait(attempt=n, reason)   # 仅 temporary + retryable
  -> Executing(snapshot, attempt=n+1)
  -> Succeeded(result)
  -> FailedPermanent(error)
  -> FailedPersistent(error, attempts)
  -> Cancelled(finality: no-effect | aborted | uncertain)
```

不变量：同一执行实例始终使用同一个 composition snapshot；派生句柄不得修改该 snapshot。

## 7. 关键不变量

1. 每次 dispatch 前能力校验与策略链看到的是同一组合结果。
2. 重试不会把一个已明确成功的副作用重复执行；对无法判定成功的操作必须标记 uncertain 或禁止重试。
3. 所有策略都能看到必要的操作上下文，但日志不得泄露 secret。
4. 并发限制必须限制真实资源使用，而不只是入口计数。
5. 测试故障注入不得残留在默认生产配置。

## 8. 边界条件

- 用户取消发生在重试等待中：立即停止等待，不发起下一次尝试。
- 超时发生在业务已发出请求后：结果不确定；是否可重试取决于幂等性和 reconcile 协议。
- 并发限制已满：排队或失败必须有显式语义；不得无限隐性排队。
- 嵌套超时：内层触发表示单次尝试超时，外层触发表示总预算耗尽。
- 错误上下文策略在 retry 外：应能看到最终 persistent 状态和尝试次数。

## 9. 本质机制 vs 实现选择

### 本质必须保留

- 输入/资源/策略分离；
- 有序组合；
- dispatch snapshot；
- 派生不影响旧操作；
- 明确的 retry/cancellation 合同；
- wrapper 转发未替换资源。

### 实现选择

- 是否使用 interface/trait/middleware；
- 是否把策略公开给最终用户；
- 策略注册方式；
- 是否支持动态配置；
- 是否有 facade 或 builder。

## 10. 行为验收场景

1. timeout 在 retry 内：每次尝试独立超时，总次数符合配置。
2. retry 在 timeout 内：总耗时不超过总预算。
3. 替换 executor/runner 后，原句柄仍用旧资源，派生句柄用新资源。
4. wrapper 替换一个资源时，另一个下层资源仍被调用。
5. 取消处于重试等待的操作：没有新尝试，最终状态为 cancelled。
6. 并发限制为 1：并发调用真实串行或按契约排队/失败。
7. chaos 注入 100% 失败：重试、错误分类、日志都按同一管线发生。
8. 非幂等写操作超时：不会被静默重试；报告 uncertain 或 permanent 状态。

## 11. 反例

以下实现不满足本规格：

- 每个业务函数内手写 `sleep + retry`，顺序不可审计；
- 修改全局 client 导致在途请求换到另一个后端；
- timeout 只检查未来是否完成，不能唤醒卡在系统调用中的执行；
- 测试通过 monkey patch 改业务内部，而不是组合边界；
- retry 对所有错误一视同仁。
