# 安装与升级指南

**[简体中文](install-upgrade.md) · [English](../install-upgrade.md) · [文档中心](README.md)**

本文覆盖安装方式、更新通道、全部受支持的升级路径，以及破坏性 / 不兼容升级前后的处理方式。

---

## 0. 早期阶段策略：新安装默认 `dev`

Rolling Memory 仍处于早期开发阶段，所以新安装默认使用 **`dev`** 更新通道，跟随 `main` 最新提交。这是有意为之：修复与兼容性工作会先落到 `main`，项目希望早期用户不必等正式发版就能拿到。

等社区壮大、系统稳定、发版节奏固定之后，再重新评估这个默认值。在那之前：

- 追求最大稳定性的用户可手动切换到 `stable`；
- 想提前验证下一版的用户可用 `beta`；
- 通道会保存为持久偏好，不是一次性参数。

默认通道不会自动安装任何东西。`dev` 只报告 `main@<commit>`，并要求用户显式执行源码重建。

---

## 1. 安装方式

### 1.1 预编译安装（推荐给大多数用户）

```bash
# macOS / Linux
curl -fsSL https://github.com/rocareer/rolling-memory/raw/main/install.sh | bash
```

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File install.ps1
```

安装脚本优先走发布渠道，并用 `SHA256SUMS` 校验每个产物。如果发布渠道没有当前平台的可验证产物，则回退到源码克隆 + 编译（需要 Git 与 Go）。

安装位置：

- CLI：`~/.local/share/rolling-memory/bin/rolling-memory`（Windows：`%LOCALAPPDATA%\rolling-memory\bin`）；
- 安装来源：`~/.local/share/rolling-memory/install.json`（`method`、`ref`、`commit`、`asset`）；
- 控制台：执行 `rolling-memory panel` 或 `update --with-console` 后位于 `~/.local/share/rolling-memory/admin/`。

### 1.2 从标签或 `main` 源码安装

```bash
# 最新开发版 main
ROLLING_MEMORY_SOURCE_REF=main bash install.sh
# 或指定正式标签
ROLLING_MEMORY_SOURCE_REF=v0.0.6 bash install.sh
# 等价参数
bash install.sh --ref v0.0.6
```

```powershell
powershell -ExecutionPolicy Bypass -File install.ps1 --ref main
```

源码安装需要 Git 与 Go。安装脚本会把源码 commit 写入 `install.json`，`dev` 通道正是靠它比较 `main` 最新提交与已安装提交。

### 1.3 离线 / 便携安装

离线包与便携布局见 [usb-portable.md](usb-portable.md)。离线安装可以正常使用；更新检查在恢复网络前会如实报告渠道不可达。

---

## 2. 更新通道

| 通道 | 来源 | 适用场景 |
|---|---|---|
| `stable` | 正式发布标签 / Release | 生产使用；最大可预测性 |
| `beta` | 含预发布标签的发布版本 | 在保留 SHA-256 校验的前提下验证候选版本 |
| `dev`（默认） | `main` 最新提交 | 早期用户；源码构建；最快拿到修复 |

保存通道偏好：

```bash
rolling-memory config set update.channel stable
rolling-memory config set update.channel beta
rolling-memory config set update.channel dev
```

或只对当前进程生效：

```bash
export ROLLING_MEMORY_UPDATE_CHANNEL=stable
```

也可以在控制台：**版本与更新 → 更新通道** 中切换。

单次检查覆盖、不保存：

```bash
rolling-memory update --check --channel stable
```

默认值：

- 没有配置文件、没有环境变量 → `dev`；
- 已有配置文件但没有 `[update]` 段 → `dev`；
- `beta` / `stable` 始终可以手动选择。

### 2.1 `dev` 到底怎么工作

`dev` 比较已安装源码 commit 与当前 `main` HEAD：

- 有 commit 记录的源码 / 本地构建：报告 `main@<commit>`，并判断是否与已安装 commit 不同；
- 没有 commit 记录的发布版安装：报告当前 `main@<commit>`，并提示先做一次源码基线；不会永远反复打扰，也不会把发布版二进制假装成源码构建。

`dev` 不会自动下载或替换二进制。控制台会禁用「立即升级」，并给出精确的重建命令：

```bash
ROLLING_MEMORY_SOURCE_REF=main bash install.sh
```

### 2.2 `stable` / `beta` 怎么工作

两者都使用正式发布产物，并在替换前用 `SHA256SUMS` 校验。`beta` 额外接受预发布标签。只有标签、没有附件的版本会如实报告为“仅源码更新”，而不是给出一个必然失败的下载。

### 2.3 更新源

`update.source` 选择发布标签与产物的来源仓库：

| 更新源 | 适用场景 |
|---|---|
| `github`（默认） | GitHub Releases 官方渠道 |
| `gitee` | Gitee 镜像，适合访问 GitHub 较慢的网络 |

可用 `rolling-memory config set update.source gitee` 保存，也可在 **版本与更新 → 更新源** 中选择。单次检查可加 `--source gitee`。显式的 `--release-base` URL 或 `ROLLING_MEMORY_RELEASE_BASE` 仍然优先。Gitee 上只有标签、尚未附带产物时，检查会如实报告“仅源码更新”，不会给出无法成功的下载。

---

## 3. 升级方式

### 3.1 命令行

```bash
# 只读检查
rolling-memory update --check

# 只查最新标签，不下载产物
rolling-memory update --check --tags-only

# 指定通道检查
rolling-memory update --check --channel beta

# 执行更新（CLI + 已安装控制台 + 宿主接线）
rolling-memory update

# 同时安装或更新本地 Web 控制台
rolling-memory update --with-console

# 机器可读输出
rolling-memory update --check --json
```

`update` 的别名是 `upgrade`。升级不会修改、清空或覆盖本地 SQLite 金库（`memory.db`）。可用 `--source gitee` 从 Gitee 镜像读取发布标签，或显式传入 `--release-base` URL。

### 3.2 控制台

从记忆总览或记忆服务页打开 **版本与更新**：

1. 有新版本时按钮显示红点；
2. 弹窗展示当前通道、最新版本、组件指纹与可安装性；
3. **检查** 执行一次显式检查；
4. **立即升级** 执行与 CLI 相同的 SHA-256 校验升级；
5. 如果控制台自身被替换，会平滑重启并刷新页面。

控制台通过本地实时通道接收版本状态，不做轮询。控制台关闭时，下次打开会立即显示缓存状态，并在后台补一次过期检查。

### 3.3 可选的本机定时检查

```bash
rolling-memory update --schedule install
rolling-memory update --schedule status
rolling-memory update --schedule remove
```

会注册当前用户级任务（macOS launchd、Linux systemd user timer、Windows 任务计划程序），每六小时刷新缓存，并对每个新版本发送一次桌面通知。它不会自动安装更新，且沿用已保存的 `update.channel` 偏好。

### 3.4 重新执行安装脚本

源码安装的受支持重建路径就是重新执行安装脚本：

```bash
ROLLING_MEMORY_SOURCE_REF=main bash install.sh
# 或指定正式标签
ROLLING_MEMORY_SOURCE_REF=v0.0.6 bash install.sh
```

预编译安装也可在发布渠道出现新产物、或需要更换安装目录时重新执行安装脚本。

### 3.5 `go install` 安装

如果 CLI 是用 `go install` 安装的：

```bash
go install github.com/rocareer/rolling-memory/cmd/rolling-memory@main
# 或指定标签
go install github.com/rocareer/rolling-memory/cmd/rolling-memory@v0.0.6
```

这种方式可能没有 `install.json`，控制台会把它视为缺少源码基线，并提示显式重建。

---

## 4. 破坏性升级与不兼容升级

### 4.1 升级前

- 阅读 `CHANGELOG.md`，重点看 **Changed**、**Removed** 与迁移说明；
- 备份金库：

```bash
cp ~/.local/share/rolling-memory/memory.db \
   ~/.local/share/rolling-memory/memory.db.bak-$(date +%Y%m%d-%H%M%S)
```

- 如需回滚，保留当前二进制：

```bash
cp ~/.local/share/rolling-memory/bin/rolling-memory \
   ~/.local/share/rolling-memory/bin/rolling-memory.previous
```

### 4.2 表结构与数据兼容性

- 表结构迁移在打开金库时幂等执行；
- 旧版本无法读取新版本已迁移的表结构，CLI 会明确报 `database schema vN is newer than this build; upgrade Rolling-Memory`；应升级二进制，而不是把文件“降级”；
- 如果新版本已经完成迁移，降级二进制可能需要恢复升级前的金库备份；
- 升级不会删除金库；背景文件、情景历史与已结晶记忆仍保存在同一个本地文件。

### 4.3 大版本与不兼容变更

项目遵循 Semantic Versioning。大版本可能修改 CLI 参数、配置键、磁盘表结构或宿主接线，`CHANGELOG.md` 与发布说明会明确标注。大版本升级建议：

1. 备份金库与二进制；
2. 阅读迁移说明；
3. 先升级 CLI，再升级控制台；
4. 打开一次金库，让迁移完成；
5. 运行 `rolling-memory doctor` 与 `rolling-memory check`。

### 4.4 降级

`update` 拒绝用旧通道版本覆盖更新的本地构建；`--force` 是面向恢复与测试的显式覆盖：

```bash
rolling-memory update --force --release-base https://github.com/rocareer/rolling-memory/releases/download/v0.0.5
```

源码安装应直接重建目标标签：

```bash
ROLLING_MEMORY_SOURCE_REF=v0.0.5 bash install.sh
```

如果新版本已经完成不兼容迁移，降级不能替代恢复金库备份。

---

## 5. 校验与安全边界

- `stable` / `beta`：每个下载产物都与渠道的 `SHA256SUMS` 比对；不匹配立即中止。
- `dev`：没有发布校验和，Git commit 就是锚点；`dev` 有意设计为源码构建路径，绝不静默替换二进制。
- 本地守护进程与控制台二进制采用原子替换；下载失败会保留原二进制。
- 文件替换后，运行中的守护进程可能仍映射上一版二进制。`rolling-memory install` 会报告该状态并打印恢复命令；`rolling-memory daemon restart` 会核验同用户套接字并启动当前二进制。
- 更新检查是本流程唯一的网络出口；记忆读写、召回、结晶与图谱保持离线。
- `ROLLING_MEMORY_UPDATE_CHECK=off` 可关闭后台检查；显式 `update --check` 仍然可用。
- `ROLLING_MEMORY_UPDATE_API_BASE` 可改用企业镜像做版本发现；`ROLLING_MEMORY_RELEASE_BASE` 可改用产物镜像。

---

## 6. 排障

| 现象 | 含义 | 处理 |
|---|---|---|
| `release assets are unavailable` | 标签存在，但渠道没有当前平台的二进制 | 等资产发布后切回 `stable` / `beta`，或源码重建 |
| `needs_source_baseline` | `dev` 通道下的发布版安装没有源码 commit | 执行一次 `ROLLING_MEMORY_SOURCE_REF=main bash install.sh` |
| `dev channel requires a source rebuild` | `dev` 不会自动替换二进制 | 使用提示的安装命令 |
| `local version is newer than channel` | 本地构建比所选通道更新 | 保留本地版本；仅在有意降级时使用 `--force` |
| `database schema is newer than this build` | 金库已被新版本迁移 | 升级二进制；降级时恢复金库备份 |
| 定时检查没有运行 | 系统计划任务不可用或已被移除 | `rolling-memory update --schedule status`，必要时重新安装 |

---

## 7. 总结

- 默认：`dev`，跟随 `main`；不会自动替换。
- `stable` / `beta`：正式发布标签，SHA-256 校验。
- 所有升级路径都是显式的：CLI、控制台、定时检查、安装脚本。
- 大版本或不兼容升级前，先备份金库与二进制。
- 等社区与发版节奏成熟后，再重新评估默认通道。
