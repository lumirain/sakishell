import React, { useEffect, useState } from "react";
import {
  GetExecutablePath,
  GetMCPStatus,
  StartMCPServer,
  StopMCPServer,
} from "../../wailsjs/go/app/App";

interface MCPStatus {
  running: boolean;
  port: number;
  url: string;
  token: string;
}

/** 各 AI 客户端的 MCP 接入配置模板 */
function buildTemplates(exe: string, url: string): { name: string; desc: string; lang: string; code: string }[] {
  const stdioConfig = (label: string, indent: string) =>
    `${indent}"${label}": {\n${indent}  "command": "${exe}",\n${indent}  "args": ["mcp", "--stdio"]\n${indent}}`;

  return [
    {
      name: "Claude Desktop",
      desc: "编辑 claude_desktop_config.json 的 mcpServers 字段",
      lang: "json",
      code: `{\n  "mcpServers": {\n${stdioConfig("sakishell", "    ")}\n  }\n}`,
    },
    {
      name: "Cline (VSCode)",
      desc: "在 Cline 设置 → MCP Servers → Add，选 stdio 模式",
      lang: "json",
      code: `{\n  "mcpServers": {\n${stdioConfig("sakishell", "    ")}\n  }\n}`,
    },
    {
      name: "Cursor",
      desc: "编辑项目 .mcp.json（或全局 ~/.cursor/mcp.json）",
      lang: "json",
      code: `{\n  "mcpServers": {\n${stdioConfig("sakishell", "    ")}\n  }\n}`,
    },
    {
      name: "Cherry Studio",
      desc: "设置 → MCP 服务器 → 添加，选 Streamable HTTP 并粘贴 URL",
      lang: "json",
      code: `{\n  "mcpServers": {\n    "sakishell": {\n      "type": "http",\n      "url": "${url}"\n    }\n  }\n}`,
    },
    {
      name: "豆包 / OpenCode / 其他",
      desc: "添加 stdio 命令：<exe> mcp --stdio；或 HTTP 方式粘贴 URL",
      lang: "text",
      code: `方式一（stdio，推荐）：\n  命令: ${exe}\n  参数: mcp --stdio\n\n方式二（HTTP 网址）：\n  ${url}`,
    },
  ];
}

interface MCPGuideProps {
  onClose: () => void;
}

export default function MCPGuide({ onClose }: MCPGuideProps) {
  const [status, setStatus] = useState<MCPStatus | null>(null);
  const [exe, setExe] = useState("");
  const [portInput, setPortInput] = useState("");
  const [tokenInput, setTokenInput] = useState("");
  const [msg, setMsg] = useState("");
  const [copied, setCopied] = useState("");

  const refresh = () => {
    void GetMCPStatus().then(setStatus);
    void GetExecutablePath().then(setExe);
  };
  useEffect(refresh, []);

  const start = async () => {
    setMsg("");
    try {
      const port = portInput ? parseInt(portInput) : 0;
      const s = await StartMCPServer(port, tokenInput);
      setStatus(s);
      setMsg(`已启动，端口 ${s.port}`);
    } catch (e) {
      setMsg(`启动失败：${e}`);
    }
  };

  const stop = async () => {
    await StopMCPServer();
    setStatus({ running: false, port: 0, url: "", token: "" });
    setMsg("已停止");
  };

  const copy = async (text: string, key: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      setMsg("复制失败，请手动选择复制");
    }
  };

  const url = status?.running ? `http://127.0.0.1:${status.port}/mcp` : "http://127.0.0.1:<端口>/mcp";
  const templates = buildTemplates(exe || "<可执行文件路径>", url);

  return (
    <div className="guide-backdrop">
      <div className="guide">
        <div className="guide-header">
          <h3>AI 接入向导</h3>
          <span className="guide-sub">让外部 AI 客户端通过 MCP 操控你的服务器</span>
          <button className="btn" onClick={onClose}>
            关闭
          </button>
        </div>

        <section className="guide-section">
          <h4>① 启动 MCP 服务</h4>
          <p className="guide-tip">AI 客户端将连接本工具提供的 MCP 接口（工具不内置模型，AI 逻辑在客户端侧）。</p>
          <div className="guide-row">
            <label>
              端口（留空自动分配）
              <input value={portInput} onChange={(e) => setPortInput(e.target.value)} placeholder="例如 9813" />
            </label>
            <label>
              访问令牌（可选，局域网暴露时建议设置）
              <input value={tokenInput} onChange={(e) => setTokenInput(e.target.value)} placeholder="Bearer token" />
            </label>
            <div className="guide-btns">
              {status?.running ? (
                <button className="btn btn-primary" onClick={stop}>
                  停止服务
                </button>
              ) : (
                <button className="btn btn-primary" onClick={start}>
                  启动 HTTP 服务
                </button>
              )}
            </div>
          </div>
          <div className={`guide-status ${status?.running ? "guide-on" : "guide-off"}`}>
            {status?.running
              ? `运行中：${status.url}${status.token ? "（已启用 Bearer 鉴权）" : ""}`
              : "未启动（stdio 方式无需启动 HTTP 服务，直接配置即可）"}
          </div>
          {msg && <div className="guide-msg">{msg}</div>}
        </section>

        <section className="guide-section">
          <h4>② 选择接入方式</h4>
          <div className="guide-methods">
            <div className="guide-method">
              <b>方式 A：stdio（推荐）</b>
              <p>本地客户端（Claude Desktop / Cline / Cursor 等）直接运行本程序作为 MCP server，不依赖 HTTP 端口。</p>
              <code className="guide-code">
                {exe} mcp --stdio
              </code>
            </div>
            <div className="guide-method">
              <b>方式 B：HTTP 网址</b>
              <p>先在①启动服务，然后把 URL 填给客户端（Cherry Studio 等支持 HTTP 的客户端）。</p>
              <code className="guide-code">{url}</code>
            </div>
          </div>
        </section>

        <section className="guide-section">
          <h4>③ 各客户端配置模板（一键复制）</h4>
          <p className="guide-tip">AI 接入后可调用工具：ssh_ai_session_open（打开 AI 共享会话）、ssh_exec（执行命令）、ssh_sftp_*（文件传输）。</p>
          {templates.map((t) => (
            <div className="guide-template" key={t.name}>
              <div className="guide-template-head">
                <b>{t.name}</b>
                <span>{t.desc}</span>
                <button className="btn" onClick={() => copy(t.code, t.name)}>
                  {copied === t.name ? "✓ 已复制" : "复制"}
                </button>
              </div>
              <pre className="guide-pre">{t.code}</pre>
            </div>
          ))}
        </section>
      </div>
    </div>
  );
}
