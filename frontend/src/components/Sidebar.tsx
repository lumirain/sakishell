import React, { useEffect, useMemo, useState } from "react";
import { ListConnections, DeleteConnection } from "../../wailsjs/go/app/App";
import type { store } from "../../wailsjs/go/models";
import { useTheme } from "../theme";

interface SidebarProps {
  onOpen: (conn: store.Connection) => void;
  onEdit: (conn: store.Connection) => void;
  onNew: () => void;
  onSFTP: (conn: store.Connection) => void;
  onGuide: () => void;
}

export default function Sidebar({ onOpen, onEdit, onNew, onSFTP, onGuide }: SidebarProps) {
  const [conns, setConns] = useState<store.Connection[]>([]);
  const { theme, toggleTheme } = useTheme();

  const load = () => {
    void ListConnections().then(setConns).catch((e) => console.error("加载连接失败", e));
  };
  useEffect(() => {
    load();
    window.addEventListener("connections-changed", load);
    return () => window.removeEventListener("connections-changed", load);
  }, []);

  // 按分组归类
  const groups = useMemo(() => {
    const map = new Map<string, store.Connection[]>();
    for (const c of conns) {
      const g = c.group || "默认";
      if (!map.has(g)) map.set(g, []);
      map.get(g)!.push(c);
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0], "zh"));
  }, [conns]);

  const remove = async (c: store.Connection) => {
    if (!confirm(`删除连接 "${c.name}"？此操作不影响服务器端。`)) return;
    try {
      await DeleteConnection(c.id);
      load();
    } catch (e) {
      alert(`删除失败: ${e}`);
    }
  };

  return (
    <aside className="sidebar">
      <div className="sidebar-header">
        <span className="brand">SakiShell</span>
      </div>

      <button className="btn btn-primary btn-block" onClick={onNew}>
        + 新建连接
      </button>

      <div className="conn-list">
        {groups.length === 0 && <div className="conn-empty">还没有连接，点击上方新建</div>}
        {groups.map(([group, items]) => (
          <div key={group} className="conn-group">
            <div className="conn-group-title">{group}</div>
            {items.map((c) => (
              <div key={c.id} className="conn-item">
                <div className="conn-info" onClick={() => onOpen(c)}>
                  <div className="conn-name">{c.name}</div>
                  <div className="conn-meta">
                    {c.username}@{c.host}:{c.port}
                  </div>
                </div>
                <div className="conn-actions">
                  <button title="文件管理 (SFTP)" onClick={() => onSFTP(c)}>
                    ⇄
                  </button>
                  <button title="编辑" onClick={() => onEdit(c)}>
                    ✎
                  </button>
                  <button title="删除" onClick={() => remove(c)}>
                    ✕
                  </button>
                </div>
              </div>
            ))}
          </div>
        ))}
      </div>

      <div className="sidebar-footer">
        <button className="btn btn-block" onClick={onGuide}>
          🤖 AI 接入向导
        </button>
        <button className="btn" onClick={toggleTheme}>
          {theme === "dark" ? "🌙 暗色" : "☀️ 亮色"}｜点击切换
        </button>
      </div>
    </aside>
  );
}
