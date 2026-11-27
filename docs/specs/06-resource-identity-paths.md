# SPEC-006：资源身份与路径边界规范化

## 1. 目标能力

系统在用户输入、文件系统、URI、外部工具输出和内部状态之间保持一致的 resource identity。同一资源不会因为 symlink、大小写、分隔符、相对路径、URI encoding 或显示格式差异被当成多个资源；不同资源也不会被错误合并。路径操作必须防止根目录逃逸。

## 2. 设计意图

- 字符串路径在跨边界时会丢失语义：absolute、relative、URI、display 容易混用。
- 编辑器/IDE 类系统中，同一文档身份错误会导致 buffer 分裂、诊断串档、保存覆盖错文件。
- 根目录边界是安全与正确性问题，不只是格式问题。
- 规范化必须发生在边界，而不是依赖每个消费者记得调用 helper。

## 3. 核心定义

- **Canonical root**：系统认定的逻辑根目录身份。
- **Real root**：解析 symlink 后的物理根目录路径（如平台适用）。
- **Absolute path**：操作系统语义下的绝对路径。
- **Root-relative path**：相对 canonical root 的规范化路径，不以下级实现分隔符为准。
- **Resource URI**：跨协议使用的 URI 身份。
- **Display path**：给用户看的格式，不得作为身份。
- **Confined join**：把相对路径安全地接到根目录内。

## 4. Normative Requirements

### REQ-PATH-010：系统 MUST 明确至少三类路径语义

实现可以不使用独立类型，但必须能区分：

1. OS absolute path；
2. root-relative logical path；
3. display/UI path。

如使用 URI，还必须区分 URI 与本地路径。禁止在文档、索引、缓存键中混用未规范化字符串。

### REQ-PATH-020：外部输入 MUST 在边界解析和验证

用户输入、配置、工具输出、URI、watcher event、拖放路径 MUST 在进入内部状态前规范化/验证。内部模块不应各自重复实现一半规则。

### REQ-PATH-030：根内相对路径 MUST 有统一语法

系统 MUST 定义分隔符、`.`、`..`、重复分隔符、尾部分隔符、空路径、根路径的规则。至少：

- `..` 不得逃逸根；
- 重复分隔符应规范化；
- 根与空路径语义不得混淆；
- 目录标记（如尾部 `/`）是否保留必须明确。

### REQ-PATH-040：confined join MUST 防止逃逸

把 root-relative path 转为 OS path 时，MUST 拒绝或规范化后拒绝：

- parent traversal 逃逸；
- absolute path 覆盖根；
- 平台 prefix/device path；
- symlink 最终逃逸（若系统承诺物理 confinement）。

仅做字符串拼接不满足本要求。

### REQ-PATH-050：symlink 身份策略 MUST 明确

系统 MUST 选择并文档化：

- logical identity：用户看到的 symlink 路径即身份；
- physical identity：realpath 后路径即身份；
- dual identity：UI 用 logical，外部工具用 physical，并有稳定映射。

不得在不同模块中混用三种策略。

### REQ-PATH-060：大小写和 Unicode 归一化策略 MUST 明确

系统 MUST 根据目标平台定义路径比较规则。不能假定所有文件系统大小写敏感或不敏感。若无法可靠检测，必须采用保守策略并说明限制。

### REQ-PATH-070：URI encoding/decoding MUST 只发生一次

file URI、remote URI 或协议 URI 的 percent-encoding、Windows drive、UNC、authority 规则 MUST 在边界处理。重复 decode 或把 URI 当 OS path 视为缺陷。

### REQ-PATH-080：缓存和文档身份 MUST 使用 canonical identity

打开文档、diagnostics、local history、quick open index、git status、watcher event 等 MUST 使用同一 canonical identity。display path MAY 作为附加字段，不得作为 key。

### REQ-PATH-090：重命名/移动 MUST 更新身份或明确建立新身份

系统 MUST 定义 rename 是同一资源身份延续，还是旧资源关闭+新资源创建。两者都可以，但缓存、历史、诊断和 watcher 行为必须一致。

## 5. 推荐边界流程

```text
external input
  -> parse by input kind (user text / OS path / URI / tool output)
  -> normalize lexical form
  -> resolve root relation and symlink policy
  -> validate confinement/security
  -> produce canonical identity
  -> internal state uses identity only
  -> UI/export converts identity back to display or URI at boundary
```

## 6. 状态与映射

```text
UnresolvedInput
  -> Invalid(reason)
  -> ExternalOutsideRoot
  -> CanonicalIdentity(root id, relative path, physical path optional)
  -> DisplayRepresentation
  -> UriRepresentation
```

映射不变量：

- canonical -> display -> canonical 可以损失格式，但不得改变资源身份；
- canonical -> URI -> canonical 必须 round-trip；
- outside-root 输入不得被静默裁剪到根内；
- display 字符串不得反向覆盖 canonical identity。

## 7. 边界条件

- root 本身是 symlink；
- 文件或中间目录是 symlink；
- 大小写不敏感文件系统中同一文件多种写法；
- Windows drive/UNC 与 POSIX path；
- trailing slash 表示目录还是无意义；
- path 包含 `%`、`?`、`#`、空格、非 ASCII；
- watcher 报告旧路径或新路径；
- 工具输出相对其 working directory 而非 root；
- root 切换后旧 identity 失效。

## 8. 本质机制 vs 实现选择

### 本质必须保留

- 边界 parse/normalize/validate；
- canonical identity；
- confinement；
- symlink/大小写/URI 策略；
- 缓存键一致；
- rename 身份语义。

### 实现选择

- 是否使用强类型；
- 是否保存 realpath；
- URI 库；
- 平台检测方式；
- display 格式。

## 9. 行为验收场景

1. 通过 symlink 路径和 realpath 打开同一文件：按契约显示为一个身份或两个身份，缓存/诊断不冲突。
2. 输入 `../outside`：被拒绝或明确 outside-root，不能拼成根外写入。
3. URI 中空格/Unicode/百分号：round-trip 后打开同一文件。
4. Windows/POSIX 分隔符混合：规范化结果一致或明确非法。
5. watcher rename：文档状态按契约迁移，无重复 buffer。
6. 大小写变体：在对应平台上按文件系统语义处理。
7. display path 修改 UI 格式：缓存键和保存目标不变。

## 10. 需求追踪示例

| Req ID | 行为证据 | 合格标准 |
| --- | --- | --- |
| REQ-PATH-040 | 构造 `root/a/../../outside` | 写入/读取被拒绝，不访问 outside |
| REQ-PATH-080 | symlink/realpath 两种方式打开 | 不出现两个互不知情的 buffer，除非契约为 dual identity |
| REQ-PATH-070 | `%` 与 `%25` 文件名 | URI round-trip 正确，无重复 decode |
