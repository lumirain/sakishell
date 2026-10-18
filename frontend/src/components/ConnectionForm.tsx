import React, { useState } from "react";
import { SaveConnection } from "../../wailsjs/go/app/App";
import type { store } from "../../wailsjs/go/models";

interface ConnectionFormProps {
  /** 为 null 表示新建，否则为编辑 */
  initial: store.Connection | null;
  onSaved: (conn: store.Connection) => void;
  onCancel: () => void;
}

export default function ConnectionForm({ initial, onSaved, onCancel }: ConnectionFormProps) {
  const [form, setForm] = useState<store.Connection>(
    initial ?? {
      id: "",
      name: "",
      group: "",
      host: "",
      port: 22,
      username: "",
      authType: "password",
      password: "",
      keyPath: "",
    }
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const set = (patch: Partial<store.Connection>) => setForm((f) => ({ ...f, ...patch }));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      const saved = await SaveConnection(form);
      onSaved(saved);
    } catch (err) {
      setError(String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <form className="modal" onClick={(e) => e.stopPropagation()} onSubmit={submit}>
        <h3>{initial ? "编辑连接" : "新建连接"}</h3>

        <label>
          名称 *
          <input value={form.name} onChange={(e) => set({ name: e.target.value })} placeholder="例如：生产服务器" required />
        </label>

        <label>
          分组
          <input value={form.group} onChange={(e) => set({ group: e.target.value })} placeholder="例如：生产 / 测试（留空为默认组）" />
        </label>

        <div className="form-row">
          <label>
            主机 *
            <input value={form.host} onChange={(e) => set({ host: e.target.value })} placeholder="IP 或域名" required />
          </label>
          <label>
            端口
            <input
              type="number"
              min={1}
              max={65535}
              value={form.port}
              onChange={(e) => set({ port: parseInt(e.target.value) || 22 })}
            />
          </label>
        </div>

        <label>
          用户名 *
          <input value={form.username} onChange={(e) => set({ username: e.target.value })} placeholder="root / ubuntu" required />
        </label>

        <div className="form-row">
          <label className="radio">
            <input type="radio" checked={form.authType === "password"} onChange={() => set({ authType: "password" })} />
            密码认证
          </label>
          <label className="radio">
            <input type="radio" checked={form.authType === "key"} onChange={() => set({ authType: "key" })} />
            密钥认证
          </label>
        </div>

        {form.authType === "password" ? (
          <label>
            密码
            <input type="password" value={form.password ?? ""} onChange={(e) => set({ password: e.target.value })} placeholder="SSH 登录密码" />
          </label>
        ) : (
          <label>
            私钥路径
            <input value={form.keyPath ?? ""} onChange={(e) => set({ keyPath: e.target.value })} placeholder="如 /home/user/.ssh/id_ed25519" />
          </label>
        )}

        {error && <div className="form-error">{error}</div>}

        <div className="modal-actions">
          <button type="button" className="btn" onClick={onCancel}>
            取消
          </button>
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </form>
    </div>
  );
}
