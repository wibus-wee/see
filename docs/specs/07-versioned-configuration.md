# SPEC-007：版本化配置契约

## 1. 目标能力

系统能以显式、可迁移、可诊断的方式加载和保存配置。配置损坏、版本过旧、字段非法、来源冲突或外部输入不可信时，用户/调用方能得到可理解结果，而不是静默丢失配置或使用未定义默认值。

## 2. 设计意图

- 配置是用户数据和系统契约，不是随意的键值包。
- 静默重置会破坏用户信任，必须可观察、可恢复。
- 配置来源组合（默认、文件、环境、CLI、远程、单次调用）必须有确定优先级。
- 外部输入必须验证，不能把配置解析当成可执行代码或安全绕过通道。

## 3. 核心定义

- **Configuration schema**：字段、类型、默认值、约束、版本。
- **Configuration snapshot**：加载、合并、验证后的不可变配置。
- **Migration**：把旧版本配置升级到新版本。
- **Source precedence**：不同来源覆盖顺序。
- **Broken config backup**：无法解析或迁移时保留的原文件副本。
- **Trusted input boundary**：配置可能影响命令、网络、路径或凭据时的安全边界。

## 4. Normative Requirements

### REQ-CFG-010：配置 MUST 有 schema 或等价显式契约

至少定义字段名、类型、默认值、取值范围、是否必填、废弃替代字段。自由扩展字段 MUST 有命名空间或忽略/保留策略。

### REQ-CFG-020：配置 MUST 有版本或兼容策略

结构可能发生破坏性变化时 MUST 有版本字段或可推断的迁移规则。无版本旧配置如何解释 MUST 写明。

### REQ-CFG-030：加载 MUST 分阶段并保留错误类别

推荐阶段：

```text
read bytes -> parse syntax -> decode schema -> validate values -> migrate -> merge sources -> snapshot
```

每一阶段失败 MUST 区分：not found、permission、syntax invalid、schema invalid、validation failed、migration failed、source conflict。

### REQ-CFG-040：解析失败 MUST NOT 静默重置用户配置

如果配置存在但损坏，系统 MUST：

- 保留原文件或创建备份；
- 向用户/调用方报告损坏位置与后果；
- 明确本次使用默认值、拒绝启动还是进入降级模式。

只有用户确认或自动备份完成后才允许覆盖原配置。

### REQ-CFG-050：来源优先级 MUST 固定并文档化

例如：`single-use override > CLI > environment > user config > project config > defaults`。实际顺序可不同，但必须固定、可查询、可测试。

### REQ-CFG-060：非法值 MUST 产生 ConfigInvalid 类错误或有界降级

数值范围、枚举、路径、URL、正则、duration、并发数等 MUST 验证。非法单个字段是拒绝整个配置还是忽略该字段，必须按字段写入契约。

### REQ-CFG-070：配置写回 MUST 保留用户数据或明确覆盖范围

如果系统只管理部分字段，不得重写并丢弃未知字段、注释或用户手写内容，除非格式明确不支持。若必须规范化重写，应在契约中说明。

### REQ-CFG-080：影响安全的配置 MUST 在信任边界验证

命令路径、环境变量、网络地址、证书、代理、自动下载、脚本执行等 MUST 验证来源和值。配置不得让 untrusted input 提升权限或绕过安全策略。

### REQ-CFG-090：配置变更 MUST 有快照语义

运行中的系统 MUST 明确：配置加载后不可变、reload 后新操作使用新快照、还是动态订阅。不得在单次操作中途读到半旧半新的组合。

### REQ-CFG-100：迁移 MUST 可测试且可回滚到备份

迁移前 SHOULD 保留原配置备份；迁移失败不得留下半迁移文件。回滚可以是人工恢复备份，但路径和步骤必须明确。

## 5. 状态机

```text
Unloaded
  -> Loading
       -> Missing -> DefaultsSnapshot
       -> ParseFailed(backup required)
       -> ValidationFailed(errors)
       -> MigrationRequired
            -> Migrating -> MigratedSnapshot | MigrationFailed
       -> LoadedSnapshot(version)
  -> ActiveSnapshot(version)
  -> Reloading -> ActiveSnapshot(new version) | ReloadFailed(old snapshot retained)
```

关键规则：reload 失败不应破坏仍在使用的旧 snapshot；迁移失败不得覆盖原始输入。

## 6. 关键不变量

1. 用户配置不会因一次语法错误被无声清空。
2. 任意一次操作使用单一配置快照。
3. 所有默认值在 schema 中可见，不是分散常量。
4. 未知字段处理策略稳定：保留、拒绝或忽略。
5. 高权限/高风险配置必须来自受信来源或获得确认。

## 7. 边界条件

- 配置不存在与配置为空对象不同；
- JSON/YAML 语法错误；
- 字段类型错误，例如字符串写成数字；
- 旧版本缺字段；
- 新版本字段被旧版本应用读取；
- 配置目录不可写；
- 环境变量包含 secret；
- project config 指向 workspace 内可执行文件；
- 多次 reload 并发发生。

## 8. 本质机制 vs 实现选择

### 本质必须保留

- schema/version/validation；
- 分阶段错误；
- 损坏备份与通知；
- 固定 precedence；
- snapshot 语义；
- 高风险配置信任边界；
- 原子写回（可结合 SPEC-003）。

### 实现选择

- JSON/YAML/TOML/数据库；
- 是否支持注释；
- 迁移框架；
- UI 如何暴露错误；
- 配置监听器实现。

## 9. 行为验收场景

1. 首次启动无配置：生成或正确使用 defaults，不报损坏。
2. 配置语法损坏：原文件保留并备份；用户看到明确通知；系统按契约降级。
3. 非法超时/并发值：返回 validation error，不使用负值或零造成意外行为。
4. 旧版本配置：迁移成功，原文件备份，迁移后字段正确。
5. 迁移中途失败：不出现半迁移配置，旧 snapshot/备份可恢复。
6. CLI/env/file 同时设置：实际生效值符合 precedence。
7. reload 失败：继续用旧 snapshot，不进入混合状态。
8. 高风险 project config：untrusted 时不执行其中命令。

## 10. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-CFG-040 | 人为写坏配置后启动 | 原文件未丢失，有备份/提示，错误类别为 syntax invalid |
| REQ-CFG-050 | 三来源设置同一字段 | 生效值符合固定优先级，诊断能解释来源 |
| REQ-CFG-090 | reload 中途执行操作 | 单操作内配置一致，无半新半旧行为 |
