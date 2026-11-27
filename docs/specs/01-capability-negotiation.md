# SPEC-001：能力协商与功能降级

## 1. 目标能力

系统能够显式描述一个外部服务、后端、语言服务器、工具或运行环境**当前组合后实际支持什么**，并让消费者按功能声明所需能力。能力缺失时必须产生可解释的降级或 Unsupported 结果，而不是静默忽略、假成功或让整个组件失效。

## 2. 设计意图

- 把“能不能做”从调用点猜测变成可查询、可测试的一等契约。
- 允许同一个接口覆盖能力差异很大的实现。
- 防止一个可选能力缺失导致无关能力全部不可用。
- 防止 correctness-critical 参数被静默丢弃。
- 让 UI/调用方能区分“永久不支持”和“本次执行失败”。

## 3. 适用范围

适用于：存储后端、语言服务器、搜索工具、版本控制工具、远程服务、平台能力、硬件能力。不要把所有配置项都塞进能力模型；能力只表达影响可观察行为的支持与否、限制和语义变体。

## 4. 核心定义

- **Raw capability**：底层服务未经包装时的原生能力。
- **Effective capability**：经过所有策略、适配器、降级、路由后的最终能力。消费者只能依赖 effective capability。
- **Feature consumer**：使用某功能的模块，例如重命名、格式化、范围读取、递归列表。
- **Correctness-critical option**：如果系统不能理解或执行，接受它会造成错误结果的选项。
- **Best-effort option**：忽略它不会改变正确性，只影响性能、提示或体验。

## 5. Normative Requirements

### REQ-CAP-010：能力 MUST 是一等值对象或等价显式模型

系统 MUST 提供可查询的能力描述，而不是让消费者读取散落布尔字段、捕获异常后猜测、或根据实现类型做不可控分支。模型可以用 struct、record、map、bitflags 或 schema 表达。

### REQ-CAP-020：能力 MUST 描述组合后的结果

如果系统通过 wrapper、代理、路由、缓存、降级层组合能力，对外暴露的 MUST 是最终 effective capability。任何中间层改变语义时都 MUST 更新能力。

### REQ-CAP-030：能力 MUST 包含操作、变体和关键限制

至少视领域包含：

- 操作是否支持；
- 条件变体是否支持，例如 compare-and-swap、if-not-exists、range、recursive；
- 大小、并发、深度、分页、字符编码等限制；
- 返回元数据是否可信或完整。

只回答“支持某操作”但隐瞒关键限制，视为不满足。

### REQ-CAP-040：消费者 MUST 在入口声明所需能力

每个功能入口 MUST 检查自己需要的能力。生命周期管理器不得因为某个可选能力缺失而拒绝整个服务，除非该能力是该服务存在的必要条件并写入契约。

### REQ-CAP-050：能力缺失 MUST 与执行失败区分

至少暴露三类结果：

1. `unsupported`：能力声明不存在，重试无意义；
2. `failed temporary`：本次执行失败，可按错误语义重试；
3. `failed permanent`：本次执行失败且重试无意义。

UI/调用方 SHOULD 对 unsupported 提供降级、隐藏入口或解释，而不是显示为普通异常。

### REQ-CAP-060：correctness-critical option MUST NOT 被静默忽略

如果请求包含影响正确性的选项而 effective capability 不支持，系统 MUST 在开始副作用前失败，并返回 unsupported/invalid capability 类错误。只有在规格明确标记为 best-effort 时才可以忽略。

### REQ-CAP-070：能力不得虚报

只有在实现能完整满足可观察契约时才 MUST 标记支持。以下情况不得标记支持：

- 只在部分输入上正确；
- 需要调用方自己补偿并发/重试错误；
- 返回结果缺少规格要求的字段；
- 只能“尝试”执行但无法保证语义。

若确有限制，SHOULD 把限制建模为能力约束，或用文档化降级行为表示。

### REQ-CAP-080：能力 SHOULD 可被诊断和测试覆盖选择使用

系统 SHOULD 能把 effective capability 输出到日志/诊断界面，并用它选择 behavior tests 的适用断言。测试不得因为实现正好通过一次就修改能力声明。

### REQ-CAP-090：运行时能力变化 MUST 有明确语义

如果能力可能因网络、版本、配置或服务器重启而改变，系统 MUST 明确：

- 能力何时采样；
- 缓存多久；
- 变化后如何失效；
- 正在执行的操作使用哪个快照。

无动态能力的系统 MAY 将能力视为初始化后不可变，但必须写出该假设。

## 6. 状态流转

```text
Unknown
  -> Discovering
  -> Available(effective capability snapshot)
  -> Degraded(reason, reduced capability snapshot)
  -> Unsupported(required capability missing)
  -> Failed(error kind/retry status)
```

关键规则：

- `Discovering` 中不得把能力当作已确认；
- `Available` 只代表快照有效，不代表后续操作必然成功；
- `Degraded` 必须说明哪些能力减少；
- `Unsupported` 是能力结论，不是普通 I/O 错误；
- 服务重启或配置变更后可以重新进入 `Discovering`，但旧操作的语义必须仍受原快照约束或有明确重新协商规则。

## 7. 关键不变量

1. 对外展示的能力永远不比实际保证更强。
2. 消费者不会绕过 capability gate 直接调用高风险操作；若绕过，错误也必须在边界被拦截。
3. 包装层不能只看 raw capability 判断组合后能力。
4. 能力缺失不会改变数据或产生半成功副作用。
5. 同一时刻同一消费者看到的能力快照一致。

## 8. 边界条件

- 服务声明支持但真实环境拒绝：按执行错误处理，并记录 capability/backend mismatch。
- 服务版本过旧：初始化时降低能力快照，而不是每次失败后猜。
- 某操作支持但不支持条件变体：普通操作可用，条件入口必须 unsupported。
- 代理可模拟缺失能力：只有模拟满足语义保真和可预测成本时才可重新标记支持。
- 能力发现本身失败：不得默认全开；应 unknown/degraded，并按保守策略处理。

## 9. 本质机制 vs 实现选择

### 本质必须保留

- effective capability；
- 按功能入口检查；
- unsupported 与 execution failure 区分；
- correctness-critical option fail-fast；
- 不虚报能力；
- 能力快照/变化语义。

### 实现选择

- capability 是 struct、bitset、schema 还是动态 map；
- 同步/异步发现；
- UI 如何显示降级；
- 是否有独立 capability-check 层；
- 是否中心化注册能力。

## 10. 行为验收场景

1. 全能力服务：每个功能入口可用，诊断中能看到完整能力。
2. 缺失可选能力：该功能明确 unavailable；其他功能继续可用。
3. 缺失必要能力：服务初始化或功能启用失败，并说明必要条件。
4. correctness-critical option 不支持：无副作用，返回 unsupported。
5. 条件变体差异：普通操作成功，条件操作 unsupported 或明确降级。
6. 动态能力变化：刷新/重启后能力快照更新，旧请求不串用错误语义。
7. 组合层模拟能力：验证模拟在并发、部分失败、重试下仍保真；否则不得标记支持。

## 11. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-CAP-040 | 启动仅支持 hover 的服务 | hover 可用，definition 入口显示 unsupported，服务不整体失败 |
| REQ-CAP-060 | 对不支持 if-not-exists 的实现提交该选项 | 写操作未创建/修改目标，返回 unsupported |
| REQ-CAP-020 | 加路由/降级 wrapper 后读取能力 | 输出的是最终组合能力而非底层裸能力 |
