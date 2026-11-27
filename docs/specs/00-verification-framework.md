# SPEC-000：需求 Conformance 与行为等价验证框架

## 1. 目的

建立一套与实现架构无关的验证方法，用于判断目标系统是否完整、正确地复现了指定能力。验证基准有两个：

1. 原始用户需求；
2. 对应 Implementation-Agnostic Specification。

本框架禁止把“代码存在”“类型正确”“单元测试通过”视为需求完成。完成度只按**需求覆盖率**与**真实行为符合度**衡量。

## 2. 设计意图

- 防止实现者把接口形状当作行为语义。
- 防止 mock 与实现互相印证，却共同偏离真实外部世界。
- 让遗漏、缩水、误解、错误降级和无法验证项显式可见。
- 允许不同语言、架构、框架复现同一能力，同时保持外部行为一致。

## 3. 核心概念

### 3.1 Requirement

一条可独立判断真伪的行为要求。必须满足：

- 有稳定 ID；
- 使用 MUST/SHOULD/MAY；
- 描述外部可观察行为或关键内部不变量；
- 能指出触发条件、预期结果和失败表现。

### 3.2 Behavior Case

从用户或调用方角度描述的一次完整交互，包括：

- 初始状态；
- 输入或外部事件；
- 中间状态变化；
- 副作用；
- 最终可观察结果；
- 异常/取消/并发变体。

### 3.3 Evidence

真实运行产生的证据。优先级从高到低：

1. 真实系统、真实外部服务、真实文件/进程/网络的观察结果；
2. 录屏、日志、trace、文件内容、退出码、网络报文；
3. 行为测试在真实边界上的输出；
4. 集成测试或受控 fake 的输出；
5. 单元测试、静态检查、类型检查。

### 3.4 Equivalence Oracle

用于判断目标行为是否等价的标准。可以是：

- specification 中的 normative behavior；
- 参考实现的外部行为；
- 同一输入在两个系统中的状态/输出对比；
- 明确写入规格的差异豁免。

禁止把参考实现内部结构、私有函数调用顺序或依赖版本当成 oracle，除非它们影响外部行为。

## 4. Normative Requirements

### REQ-VF-010：需求追踪矩阵 MUST 覆盖全部 normative requirements

实现方 MUST 建立矩阵，字段至少包括：

| 字段 | 含义 |
| --- | --- |
| Requirement ID | 规格中的稳定 ID |
| Interpretation | 实现者对需求的解释，注明歧义 |
| User/system flow | 从哪个真实流程触发 |
| Implementation evidence | 实现位置，仅作导航，不作完成证据 |
| Behavioral evidence | 实际运行证据链接或记录 |
| Status | implemented / partial / missing / divergent / unverifiable |
| Gap | 差距、复现步骤、影响 |

### REQ-VF-020：每个 MUST 需求 MUST 至少有一条行为验收证据

行为证据 MUST 描述真实输入、初始状态、操作步骤、观察结果。仅有单元测试或类型检查不得标记 implemented。

### REQ-VF-030：验收 MUST 覆盖正常、异常、边界、取消和并发路径

至少检查：

- 空输入、最大输入、非法输入；
- 资源不存在、权限不足、冲突；
- 中途取消、超时、进程/连接中断；
- 重试是否造成重复副作用；
- 并发操作是否破坏不变量；
- 状态恢复是否与文档一致。

### REQ-VF-040：行为等价验证 MUST 从用户流程出发

验证步骤 SHOULD 使用真实用户路径，例如打开文件、编辑、保存、重启外部服务、崩溃恢复、重新加载。禁止只调用内部 helper 来宣称用户流程正确。

### REQ-VF-050：参考实现对照 MUST 只比较外部表现

如参考实现可运行，SHALL 对照：

- 同一请求的成功/失败分类；
- 状态转换顺序；
- 最终资源状态；
- 可观察错误信息类别；
- 重试、取消、超时后的结果。

不得要求目标实现复刻参考实现的包结构、类型名、内部事件顺序或线程模型。

### REQ-VF-060：无法验证项 MUST 显式列出

无法验证不等于已实现。每项 MUST 给出原因，例如缺少凭据、平台不可用、外部服务无法部署、触发条件不可复现，并给出后续验证计划。

### REQ-VF-070：偏差 MUST 分类

| 状态 | 定义 |
| --- | --- |
| implemented | 行为证据满足完整需求 |
| partial | 只覆盖部分输入、状态或平台 |
| missing | 没有实现或没有可观察行为 |
| divergent | 有行为但与规格冲突 |
| unverifiable | 有实现但当前无法取得行为证据 |
| waived | 经需求方明确豁免；必须记录理由 |

### REQ-VF-080：测试替身 MUST 证明自身 fidelity

使用 fake/emulator 时，MUST 说明它复现了真实服务的哪些行为、未复现哪些行为。涉及外部协议或平台语义时，SHOULD 至少用一次真实服务/真实工具验证关键路径。

### REQ-VF-090：验收报告 MUST 能复现

每个失败或偏差 MUST 包含：

1. 精确版本或 commit；
2. 环境；
3. 初始状态；
4. 操作步骤；
5. 实际结果；
6. 预期结果；
7. 证据位置；
8. 最小复现方式。

## 5. 标准验证流程

1. **提取需求**：从规格中列出全部 MUST/SHOULD，禁止只摘取容易实现的部分。
2. **建立用户流程地图**：把需求映射到真实入口，而不是内部函数。
3. **准备真实环境**：尽量使用真实文件系统、真实外部进程、真实网络服务。
4. **运行行为场景**：覆盖正常、异常、取消、并发、恢复。
5. **记录证据**：保存日志、录屏、文件状态、命令输出、trace。
6. **执行差距分析**：逐项标记状态并解释证据。
7. **回归验证**：修复后重跑完整流程，而不是只重跑失败用例。

## 6. 最小验收模板

```markdown
# Conformance Report

- Target revision:
- Environment:
- Specification revision:

| Req ID | Status | Behavioral evidence | Gap / reproduction |
| --- | --- | --- | --- |
| REQ-XX-010 | implemented | <link/path + observed result> | - |

## User-flow acceptance
1. Flow:
   Setup:
   Steps:
   Expected:
   Actual:
   Evidence:

## Deviations
- ID:
  Classification: missing / partial / divergent / unverifiable
  Impact:
  Reproduction:
  Evidence:

## Coverage summary
- MUST implemented: x/y
- SHOULD implemented or consciously waived: x/y
- Not acceptable as evidence: <list of only-unit-test claims>
```

## 7. 本质机制 vs 实现选择

### 本质必须保留

- 需求到真实行为的追踪；
- 真实用户流程验收；
- 异常/取消/并发路径；
- 明确的差距分类；
- 可复现证据。

### 实现选择

- 使用何种测试框架；
- 是否自动录制视频；
- 矩阵存放在 Markdown、电子表格还是测试系统；
- 行为测试命名方式；
- 参考实现是否自动启动。

## 8. 验收标准

本规格自身合格的最低标准：

- 每个目标规格均有完整追踪矩阵；
- 每个 MUST 均有真实行为证据或明确 unverifiable 状态；
- 报告能回答“哪些用户需求没有实现”，而不只是“哪些测试通过”；
- 任何 partial/divergent/missing 都能用给定步骤复现。
