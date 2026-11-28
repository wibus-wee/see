# SPEC-009：第二轮高级机制补充规格

本文件补充第一轮未覆盖的 OpenDAL 机制。它仍是 implementation-agnostic：只要求复现外部语义，不要求复刻 Rust、Operator、Service 或 crate 结构。

## A. 三段式 API 语法：便捷方法、流式选项、结构化选项

### 设计意图

同一操作通常有三种使用方式：

1. 简单调用：只传必需参数；
2. 链式/渐进式调用：逐项增加选项；
3. 结构化调用：一次传入完整 options。

三者必须共享同一语义模型，而不是形成三套行为分叉。

### 行为契约

- **REQ-API-010** 简单方法 MUST 等价于空 options 或默认 options 的完整方法。
- **REQ-API-020** 链式设置与结构化设置 MUST 进入同一个 validation/dispatch 边界，不得分别复制业务逻辑。
- **REQ-API-030** 每个选项 MUST 在完整 options 类型中有可追踪归属；链式 API 不得暴露结构化 API 不存在的隐式能力。
- **REQ-API-040** 非法或不支持的选项 MUST 在同一处失败，不能因调用形式不同产生不同结果。
- **REQ-API-050** 文档 MUST 说明默认方法与高级方法的关系，避免用户误以为高级选项不可用。

### 关键不变量

- 简单路径不是“另一个实现”，只是默认值。
- API 表面可有多种人体工学形式，行为内核只有一个。
- 新增选项时三种形式的能力保持一致，或明确只暴露高级形式。

### See 可借鉴差距

See 可应用到 search、git、language-server request、save/export、task runner：不要给每个 UI 入口复制一套参数拼装。建议每个外部操作有一个 typed options，UI/快捷键/命令面板只负责构造 options。

### 行为验收

- `read(path)` 与 `read_options(path, default)` 结果一致。
- 链式设置 `range/concurrent` 与结构化 options 相同输入产生相同请求。
- unsupported option 在三种入口下都 fail-fast。

---

## B. Metadata 与版本身份

### 设计意图

metadata 不是路径的永久属性，而是**某次操作观察到的资源状态**。同一资源在不同版本、删除标记、请求时间下可产生不同 metadata。把 metadata 当永久缓存会制造陈旧冲突和错误覆盖。

### 行为契约

- **REQ-META-010** Metadata MUST 是 immutable snapshot，并绑定产生它的操作/观察时刻或版本。
- **REQ-META-020** 若系统支持版本，metadata MUST 表达 version id、是否 current、是否 deleted/delete marker；未知必须是 unknown，不得假造 false。
- **REQ-META-030** ETag/version/checksum MUST 原样保留服务语义，不得为了显示而破坏可比较性。
- **REQ-META-040** 条件操作使用的 identity token MUST 来自同一契约来源；不得混用长度、mtime、hash、etag。
- **REQ-META-050** copy/rename/restore 是否保留 metadata MUST 明确；默认替换或保留不能因后端不同而偶然变化。
- **REQ-META-060** metadata 缺失与值为空 MUST 区分。

### 状态/不变量

```text
Unobserved -> Observed(snapshot at operation)
Observed -> Stale(after mutation/version change)
Observed + conditional request -> Commit(only if identity still matches)
```

- metadata snapshot 不得被后续操作原地修改；
- current/deleted 未知不能用 false 代替；
- successful mutation 应返回新的 identity token（若后端支持）。

### See 可借鉴差距

See 已有 fileStamp/dirty/conflict 概念，但可以升级为统一 `FileObservation`：mtime/size/hash/version/observed-at/source-operation。保存冲突、git status、watcher、local history 都应明确使用哪种 identity，而不是混合使用路径、mtime 和内容。

### 行为验收

- 文件被外部修改后，旧 metadata 明确 stale，不能用于无条件覆盖。
- delete marker/unknown version 不被当成普通文件。
- copy 后 metadata 保留/替换行为符合文档。

---

## C. 条件操作与原子性

### 设计意图

条件操作让用户只在资源仍处于预期状态时观察或修改它。它解决并发写冲突、重复创建、版本误恢复等问题。核心不是“先 stat 再写”，而是条件必须与可见提交原子绑定。

### 行为契约

- **REQ-COND-010** 条件 MUST 明确作用于哪个目标：当前文件、指定版本、源文件还是目标文件。
- **REQ-COND-020** 多条件组合 MUST 使用明确逻辑；默认应为 AND，除非协议另有说明。
- **REQ-COND-030** 条件不匹配 MUST 返回专门 condition-mismatch 类错误，不得混淆为 not-found/conflict/permission。
- **REQ-COND-040** 文件存在与不存在时的行为 MUST 分别写明。
- **REQ-COND-050** 条件 mutation MUST 在 publish/commit 边界原子判断；禁止用 `stat` + unconditional mutation 冒充。
- **REQ-COND-060** staged/multipart 写入 MUST 在最终 commit 时评估条件，而不是 begin 时。
- **REQ-COND-070** 若底层无法原子实现，该能力 MUST 不 advertise，或明确降级为较弱语义。
- **REQ-COND-080** batch 中每个条目的条件结果 MUST 独立报告，不能一个失败导致其他结果不明。

### See 可借鉴差距

普通保存、workspace replace、rename、git checkout/restore、local history restore 都适合条件语义。尤其是“保存时磁盘内容仍等于我读取时的版本”应成为保存契约的一部分，而不是 UI 提示后仍可能 race。

### 行为验收

- 读取文件后外部程序修改，保存触发 condition mismatch，目标不被覆盖。
- `create if not exists` 在目标已存在时失败且目标不变。
- staged write 期间目标被改变，最终 publish 仍检测到冲突。
- 批量 rename 一项冲突时，每项结果独立可见。

---

## D. Checksum 与端到端完整性

### 设计意图

checksum 不是普通 metadata 字段，而是写意图、提交验证和读验证的一部分。关键是明确“谁验证、何时验证、失败后目标是否可见”。

### 行为契约

- **REQ-CHK-010** checksum 必须区分 full-object digest、chunk digest、传输校验和调用方期望值。
- **REQ-CHK-020** 显式 expected checksum 表示提交前必须验证；不匹配 MUST 拒绝 commit。
- **REQ-CHK-030** pre-commit checksum rejection MUST 保持旧对象不变；无法判断是否 checksum rejection 时不得错误归类。
- **REQ-CHK-040** 读验证若启用，MUST 在交付最后字节前完成校验；失败不得让调用方误以为完整成功。
- **REQ-CHK-050** range read 与 full-object checksum 的关系必须明确；不能对部分内容错误应用全对象校验。
- **REQ-CHK-060** copy through streaming 时，源 checksum 失败后的 writer 状态必须明确：abort、可追加后继续、还是 poisoned。
- **REQ-CHK-070** checksum 能力缺失时不得静默跳过用户要求的强验证。

### See 可借鉴差距

适合用于：下载/安装语言服务器、保存/恢复 local history、workspace copy、外部工具输出缓存、update/artifact。重点不是所有文件都 hash，而是当用户或系统要求“必须与期望一致”时，失败不能留下被当成成功的新版本。

### 行为验收

- 下载文件 digest 不匹配：安装不启用，旧版本保留。
- local history 快照恢复内容损坏：恢复失败，目标不被部分覆盖。
- range 预览不因为无法 full-object checksum 而错误失败，除非用户强要求。

---

## E. 结构化 fuzz 与资源预算

### 设计意图

fuzz 不只是喂随机 bytes。高质量 fuzz 会把输入结构化为操作序列，同时给操作数、字节数、chunk 数加预算，避免 fuzz 本身造成资源爆炸。发现的真实 crash/OSS-Fuzz issue 应转成最小回归。

### 行为契约

- **REQ-FUZZ-010** fuzz input MUST 生成合法或近合法操作序列，而不是只生成随机字节后全部拒绝。
- **REQ-FUZZ-020** 每个 target MUST 有总字节、chunk、操作数、时间或内存预算。
- **REQ-FUZZ-030** 小预算参数不得组合成放大攻击，例如 chunk=1 导致数百万次底层写。
- **REQ-FUZZ-040** 发现的 fuzz failure MUST 转化为最小 regression input。
- **REQ-FUZZ-050** fuzz 不得依赖网络外部服务；真实服务行为由 conformance suite 验证，fuzz 负责本地不变量和解析健壮性。
- **REQ-FUZZ-060** fuzz assertion 应关注 public API 不 panic、不死锁、不越界、不泄露资源，而不是镜像内部实现。

### 适合 fuzz 的目标

- path normalization/confined join；
- LSP JSON-RPC frame parser/position encoding；
- ripgrep/git porcelain output parser；
- save/load/recovery 文件格式；
- regex playground 输入；
- document version/edit sequence；
- hex paging/range calculations。

### 行为验收

- chunk=1 的输入最多产生有界次数 write。
- malformed LSP frame 不会 panic、永久阻塞或耗尽内存。
- 随机 path 输入不会逃逸 root。
- 每个历史 crash 有固定 regression corpus。

---

## F. 明确威胁模型与安全边界

### 设计意图

安全不是一句“不要泄露 secret”。成熟项目会明确哪些风险在库/应用边界内，哪些由宿主或部署方负责，并在架构变化时同步更新威胁模型。

### 行为契约

- **REQ-SEC-010** 项目 MUST 写明 system model、trust boundary、in-scope、out-of-scope。
- **REQ-SEC-020** 凭证隔离 MUST 跨独立 operator/session/project 生效；一个上下文不得误用另一个上下文的 token/proxy/签名。
- **REQ-SEC-030** Debug/error/log/trace MUST 脱敏；是否允许路径出现在日志中要有默认规则和部署说明。
- **REQ-SEC-040** 不可信输入 parser MUST 不因恶意输入 panic、失控分配或泄露凭证。
- **REQ-SEC-050** 本地路径、symlink、临时目录、atomic write 是否属于安全边界 MUST 明确；不能用“只是库/编辑器”回避已承诺的行为。
- **REQ-SEC-060** 资源耗尽若默认 out-of-scope，MUST 明确要求调用方启用哪些 timeout/concurrency/bounds 控制；项目自带默认控制时则进入 in-scope。
- **REQ-SEC-070** 威胁模型 MUST 随新 surface 更新：新协议、新 signer、新本地执行、新 FFI、新遥测。

### See 可借鉴差距

See 已有 workspace trust，这是很好的起点。可继续补 `SECURITY-THREAT-MODEL.md`：

- in-scope：workspace trust、外部 executable discovery、LSP/env secret redaction、路径逃逸、配置注入、terminal command provenance、local history privacy；
- conditional：资源耗尽取决于是否启用内置 limits；
- out-of-scope：用户明确信任恶意 workspace、宿主机本身被 compromise、后端 git hosting 授权。

### 行为验收

- untrusted workspace 不执行其中二进制；
- 含 token 的 env/command 不出现在日志；
- malformed LSP/git/rg 输入不 panic、不无限分配；
- 新外部集成交付时同步更新 threat model。

---

## G. List/扫描流语义与版本化列表

### 设计意图

列表不是一次性数组，而是可能很大、分页、并发变化、含版本/删除标记的 stream。其语义包括递归、start-after、glob、版本、删除标记、重复项和错误后的终止行为。

### 行为契约

- **REQ-LIST-010** list MUST 明确 recursive true/false 的前缀语义和 trailing slash 对目录模式的影响。
- **REQ-LIST-020** stream 遇到错误后 MUST 终止或明确可恢复；不得继续产生半真半假的结果。
- **REQ-LIST-030** 大列表 MUST 支持背压/分页或显式内存上限。
- **REQ-LIST-040** version/deleted entries 只有能力支持时才可请求；unsupported MUST fail-fast。
- **REQ-LIST-050** start-after/glob/filter 是后端语义还是客户端过滤 MUST 明确；客户端过滤不得虚报后端能力。
- **REQ-LIST-060** 重复 entry、乱序、遗漏是否违反契约 MUST 写明；sanity checker 可用于开发期发现 backend contract violation。

### See 可借鉴差距

Quick Open、workspace tree、search results、git status 都面对同样问题。See 已有缓存和上限，但可以进一步明确：扫描错误后缓存是否可信、watcher 增量与全量扫描如何合并、重复 entry 是否代表 bug、哪些过滤在 rg/后端做、哪些在 UI 做。

### 行为验收

- 扫描 20 万文件时内存受控，取消后扫描停止。
- watcher 漏事件后，恢复机制不会长期展示错误缓存。
- 搜索/列表错误不会显示为“空结果”。

---

## 第二轮优先级建议

| 优先级 | 对 See 最值得关注 | 原因 |
| --- | --- | --- |
| P0 | 条件保存/原子冲突语义 | 直接防止用户源码被覆盖，是编辑器核心正确性 |
| P0 | 安全威胁模型 | See 已执行外部工具、读取 workspace、处理 secret env，需要边界文档 |
| P1 | 统一 FileObservation metadata | 让 dirty/conflict/watcher/local history/git 使用同一身份模型 |
| P1 | fuzz path/parser/save format | 成本低，能持续保护边界解析 |
| P1 | list/scan stream 契约 | Quick Open 和大 workspace 已遇到同类问题 |
| P2 | checksum 强验证 | 对安装器、local history、下载工具有价值 |
| P2 | 三段式 API options | 随外部操作复杂度增长再统一，不必先抽象 |
