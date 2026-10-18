import React, { useState } from "react";
import { OpenTerminal, OpenSFTP } from "../wailsjs/go/app/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import type { store } from "../wailsjs/go/models";
import Sidebar from "./components/Sidebar";
import ConnectionForm from "./components/ConnectionForm";
import TerminalView from "./components/TerminalView";
import AISessionView from "./components/AISessionView";
import MCPGuide from "./components/MCPGuide";
import SFTPView from "./components/SFTPView";
import { ThemeProvider } from "./theme";
import "./style.css";

/** 一个终端 Tab */
interface Tab {
  key: string; // 唯一 key（= sessionId）
  sessionId: string;
  title: string;
  type: "user" | "ai_shared" | "sftp"; // M2 起支持 AI 共享会话；M4 起支持 SFTP 面板
}

function AppInner() {
  const [tabs, setTabs] = useState<Tab[]>([]);
  const [activeKey, setActiveKey] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<store.Connection | null>(null);
  const [guideOpen, setGuideOpen] = useState(false);

  // 监听后端事件：AI 通过 MCP 打开共享会话 → 自动新建 AI 会话 Tab 并激活
  React.useEffect(() => {
    const off = EventsOn("ai:session_opened", (info: { id: string; hostPort: string; title: string }) => {
      setTabs((t) =>
        t.some((x) => x.sessionId === info.id)
          ? t
          : [...t, { key: info.id, sessionId: info.id, title: info.title || info.hostPort, type: "ai_shared" }]
      );
      setActiveKey(info.id);
    });
    return off;
  }, []);

  // 打开连接 → 新建终端 Tab
  const openConn = async (conn: store.Connection) => {
    try {
      const info = await OpenTerminal(conn.id, "user", 80, 24);
      const key = info.id;
      setTabs((t) => {
        if (t.some((x) => x.sessionId === key)) return t;
        return [...t, { key, sessionId: key, title: conn.name, type: info.type === "ai_shared" ? "ai_shared" : "user" }];
      });
      setActiveKey(key);
    } catch (e) {
      alert(`连接失败：${e}`);
    }
  };

  const closeTab = (key: string) => {
    setTabs((t) => {
      const idx = t.findIndex((x) => x.key === key);
      const next = t.filter((x) => x.key !== key);
      setActiveKey((cur) => {
        if (cur !== key) return cur;
        if (next.length === 0) return null;
        return next[Math.max(0, idx - 1)].key;
      });
      return next;
    });
  };

  const openForm = (conn: store.Connection | null) => {
    setEditing(conn);
    setFormOpen(true);
  };

  // 打开 SFTP 面板 → 新建 sftp Tab
  const openSFTP = async (conn: store.Connection) => {
    try {
      const info = await OpenSFTP(conn.id);
      const key = info.id;
      setTabs((t) => {
        if (t.some((x) => x.sessionId === key)) return t;
        return [...t, { key, sessionId: key, title: `SFTP: ${conn.name}`, type: "sftp" }];
      });
      setActiveKey(key);
    } catch (e) {
      alert(`打开 SFTP 失败：${e}`);
    }
  };

  return (
    <div className="app">
      <Sidebar onOpen={openConn} onEdit={(c) => openForm(c)} onNew={() => openForm(null)} onSFTP={openSFTP} onGuide={() => setGuideOpen(true)} />

      <main className="main">
        {tabs.length === 0 ? (
          <div className="welcome">
            <h2>SakiShell</h2>
            <p>左侧选择连接打开终端，或点击「新建连接」添加服务器。</p>
            <p>点击左下角「AI 接入向导」让 AI 客户端通过 MCP 操控服务器。</p>
          </div>
        ) : (
          <>
            <div className="tab-bar">
              {tabs.map((t) => (
                <div
                  key={t.key}
                  className={`tab ${activeKey === t.key ? "tab-active" : ""} ${t.type === "ai_shared" ? "tab-ai" : ""}`}
                  onClick={() => setActiveKey(t.key)}
                >
                  <span className="tab-title">{t.title}</span>
                  {t.type === "ai_shared" && <span className="tab-badge">AI</span>}
                  <button
                    className="tab-close"
                    onClick={(e) => {
                      e.stopPropagation();
                      closeTab(t.key);
                    }}
                  >
                    ✕
                  </button>
                </div>
              ))}
            </div>
            <div className="tab-body">
              {tabs.map((t) =>
                t.key === activeKey
                  ? t.type === "ai_shared" ? (
                      <AISessionView key={t.key} sessionId={t.sessionId} title={t.title} />
                    ) : t.type === "sftp" ? (
                      <SFTPView key={t.key} sessionId={t.sessionId} title={t.title} onClosed={closeTab} />
                    ) : (
                      <TerminalView key={t.key} sessionId={t.sessionId} title={t.title} onClosed={closeTab} />
                    )
                  : null
              )}
            </div>
          </>
        )}
      </main>

      {formOpen && (
        <ConnectionForm
          initial={editing}
          onCancel={() => setFormOpen(false)}
          onSaved={() => {
            setFormOpen(false);
            window.dispatchEvent(new Event("connections-changed"));
          }}
        />
      )}

      {guideOpen && <MCPGuide onClose={() => setGuideOpen(false)} />}
    </div>
  );
}

export default function App() {
  return (
    <ThemeProvider>
      <AppInner />
    </ThemeProvider>
  );
}
