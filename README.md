# SakiShell

带 **MCP（Model Context Protocol）** 能力的桌面端 SSH 终端连接工具。

- 技术栈：Wails v2（Go 后端）+ React/TypeScript + xterm.js
- 核心能力：工具本身即 **MCP Server**，对外暴露 SSH 操控工具，**不内置任何 AI 模型**；外部 AI 客户端（Claude Desktop / Cline / Cursor / Cherry Studio / 豆包 / OpenCode 等）通过 stdio 或 HTTP 接入后，可在**用户可见的 AI 共享会话**中操控服务器，用户也能在该会话手动输入命令，双方共享同一 shell 状态。

## 功能特性

| 模块 | 说明 |
|---|---|
| 手动 SSH 终端 | 连接管理（密码/密钥认证、分组）、标准交互式 xterm.js 终端、多 Tab |
| AI 共享会话 | AI 经 MCP 打开独立会话（新 Tab），命令与输出实时可见；用户可手动输入，与 AI 共享同一 PTY（当前目录/环境/历史一致） |
| AI 操作记录 | 每个 AI 会话右侧时间线：工具调用时间、命令（密码脱敏）、状态、耗时 |
| SFTP 文件管理 | 面板目录浏览/上传/下载/新建/重命名/删除 + 进度条；MCP 工具 `ssh_sftp_list/upload/download` |
| MCP Server | 工具 `ssh_ai_session_open` / `ssh_exec` / `ssh_sftp_*` / `ssh_session_close`；传输：**stdio**（headless）、**传统 SSE**、**Streamable HTTP**（可配端口 + Bearer token） |
| 接入向导 | UI 内一键生成各 AI 客户端配置模板（stdio 命令 / HTTP URL）并复制 |
| 主题 | 亮色 / 暗色切换，偏好持久化 |
| 安全 | Windows 凭据 DPAPI 加密；主机密钥 TOFU 校验防中间人；审计 JSONL（密码脱敏） |
| 打包 | Windows NSIS 安装包（含卸载程序）；Linux deb + AppImage |

## 运行方式

### 开发

```bash
# 依赖：Go ≥ 1.25、Node ≥ 18、Linux 需 libgtk-3-dev libwebkit2gtk-4.0-dev
wails dev
```

### 生产构建

```bash
# Linux 二进制
wails build -platform linux/amd64

# Windows NSIS 安装包（建议在 Windows 或 CI 上执行）
wails build -platform windows/amd64 -nsis
```

## 安装包

- **Windows**：`sakishell-setup-<版本>.exe`（NSIS 安装器）——安装目录、开始菜单快捷方式、添加/删除程序注册，自带 **Uninstall.exe** 卸载程序（可保留配置/审计）。
- **Linux**：`sakishell_<版本>_amd64.deb`（`dpkg -i` 安装，`dpkg -r sakishell` 卸载）与 `.AppImage`（下载后 `chmod +x` 直接运行）。

> 多平台产物由 `.github/workflows/release.yml` 在 GitHub Actions 上自动构建：推 `v*` 标签或手动触发，产物上传至 Workflow Artifacts / Release。

## 接入 AI 客户端（MCP）

### 方式 A：stdio（推荐，无需开 HTTP 服务）

在客户端 MCP 配置中把本程序作为 stdio server 启动：

```json
{
  "mcpServers": {
    "sakishell": {
      "command": "/path/to/sakishell",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

Windows 示例：`"command": "C:\\Program Files\\SakiShell\\sakishell.exe", "args": ["mcp", "--stdio"]`。

### 方式 B：HTTP 网址

在应用内打开「AI 接入向导」→ 启动 HTTP 服务（可设端口与 Bearer token），把 `http://127.0.0.1:<端口>/mcp` 填入支持 HTTP 的客户端（Cherry Studio 等）。

> 客户端适配要点：Streamable HTTP 的 `initialize` 响应会返回 `mcp-session-id`，后续 `notifications/initialized` 与 `tools/call` 需携带该 header；另兼容传统 SSE 端点 `/sse`。

### 工具清单

| 工具 | 参数 | 说明 |
|---|---|---|
| `ssh_ai_session_open` | host, port, username, auth_type(password/key), password\|key_path, tab_title? | 打开/复用 AI 共享会话，返回 session_id |
| `ssh_exec` | session_id, command, timeout? | 注入共享 PTY 执行，返回退出码+输出，终端实时可见 |
| `ssh_sftp_list` | session_id, path | 列远程目录 |
| `ssh_sftp_upload` | session_id, local_path, remote_path | 上传文件 |
| `ssh_sftp_download` | session_id, remote_path, local_path | 下载文件 |
| `ssh_session_close` | session_id | 关闭会话 |

## 数据与安全

- 数据目录：Linux `~/.config/sakishell/`、Windows `%APPDATA%/sakishell/`（`connections.json` 0600、`host_keys.json`、`audit/audit.jsonl`）
- Windows 上连接密码经 **DPAPI**（当前用户主密钥）加密后落盘；Linux 依赖文件权限 0600 + 用户隔离（可自行接入 gnome-keyring）
- 主机密钥 **TOFU**：首次连接记录公钥，后续连接校验防中间人；公钥变更时拒绝连接并提示
- 审计 JSONL：时间、来源（客户端名）、会话、工具、命令（密码脱敏）、状态、耗时

## 项目结构

```
sakishell/
├── main.go            # 入口；os.Args[1]=="mcp" → headless MCP 模式
├── app/               # Wails 绑定：连接管理、终端/会话、MCP 生命周期、SFTP
├── sshx/              # SSH 引擎：会话池、PTY、命令注入、SFTP、TOFU 主机密钥
├── mcp/               # mcp-go server：工具注册、stdio/SSE/Streamable HTTP、审计
├── store/             # 连接配置持久化 + 凭据加密（DPAPI build-tag）
├── audit/             # JSONL 审计（密码脱敏）
├── frontend/          # React + TS + xterm.js（终端/AI 会话/SFTP/向导/主题）
└── .github/workflows/ # 双平台安装包 CI
```

## 已知约束

- Windows 安装包经 GitHub Actions 的 windows runner 产出（本仓库开发环境为 Linux，无法本地交叉产出带 WebView2/NSIS 的完整安装器）。
