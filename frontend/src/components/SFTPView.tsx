import React, { useCallback, useEffect, useState } from "react";
import {
  CloseSFTP,
  SFTPDownload,
  SFTPList,
  SFTPMkdir,
  SFTPRemove,
  SFTPRename,
  SFTPUpload,
} from "../../wailsjs/go/app/App";
import type { sshx } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime/runtime";

interface SFTPViewProps {
  sessionId: string;
  title: string;
  onClosed?: (key: string) => void;
}

interface Transfer {
  action: "upload" | "download";
  name: string;
  done: number;
  total: number;
}

export default function SFTPView({ sessionId, title, onClosed }: SFTPViewProps) {
  const [path, setPath] = useState("/home/user");
  const [entries, setEntries] = useState<sshx.SFTPEntry[]>([]);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const [transfer, setTransfer] = useState<Transfer | null>(null);

  const load = useCallback(
    async (p: string) => {
      setLoading(true);
      setErr("");
      try {
        const list = await SFTPList(sessionId, p);
        setEntries(list);
        setPath(p);
      } catch (e) {
        setErr(`读取失败：${e}`);
      } finally {
        setLoading(false);
      }
    },
    [sessionId]
  );

  useEffect(() => {
    void load("/home/user");
    const off = EventsOn("sftp:progress", (p: { id: string; done: number; total: number }) => {
      if (p.id !== sessionId) return;
      setTransfer((t) => (t ? { ...t, done: p.done, total: p.total } : t));
    });
    return off;
  }, [sessionId, load]);

  useEffect(() => {
    return () => {
      void CloseSFTP(sessionId);
      if (onClosed) onClosed(sessionId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const openEntry = (e: sshx.SFTPEntry) => {
    if (e.isDir) {
      const p = path.endsWith("/") ? path + e.name : path + "/" + e.name;
      void load(p);
    }
  };

  const upload = async () => {
    const lp = prompt("本机文件完整路径（例如 /home/user/Downloads/note.txt）：");
    if (!lp) return;
    const name = lp.split(/[\\/]/).pop() || "file";
    const remote = path.endsWith("/") ? path + name : path + "/" + name;
    setTransfer({ action: "upload", name, done: 0, total: 0 });
    try {
      await SFTPUpload(sessionId, lp, remote);
      setTransfer(null);
      void load(path);
    } catch (e) {
      setErr(`上传失败：${e}`);
      setTransfer(null);
    }
  };

  const download = async (e: sshx.SFTPEntry) => {
    if (e.isDir) return;
    setTransfer({ action: "download", name: e.name, done: 0, total: 0 });
    const local = `/home/user/Downloads/${e.name}`;
    try {
      await SFTPDownload(sessionId, path.endsWith("/") ? path + e.name : path + "/" + e.name, local);
      setTransfer(null);
      setErr(`已下载到 ${local}`);
    } catch (err2) {
      setErr(`下载失败：${err2}`);
      setTransfer(null);
    }
  };

  const remove = async (e: sshx.SFTPEntry) => {
    const p = path.endsWith("/") ? path + e.name : path + "/" + e.name;
    if (!confirm(`确认删除 ${p}？`)) return;
    try {
      await SFTPRemove(sessionId, p);
      void load(path);
    } catch (e2) {
      setErr(`删除失败：${e2}`);
    }
  };

  const mkdir = async () => {
    const name = prompt("新建目录名称：");
    if (!name) return;
    try {
      await SFTPMkdir(sessionId, path.endsWith("/") ? path + name : path + "/" + name);
      void load(path);
    } catch (e2) {
      setErr(`创建失败：${e2}`);
    }
  };

  const doRename = async (e: sshx.SFTPEntry) => {
    const oldP = path.endsWith("/") ? path + e.name : path + "/" + e.name;
    const name = prompt("重命名为：", e.name);
    if (!name) return;
    const newP = path.endsWith("/") ? path + name : path + "/" + name;
    try {
      await SFTPRename(sessionId, oldP, newP);
      void load(path);
    } catch (e2) {
      setErr(`重命名失败：${e2}`);
    }
  };

  const fmtSize = (n: number) => {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
  };

  const pct = transfer && transfer.total > 0 ? Math.round((transfer.done / transfer.total) * 100) : 0;

  return (
    <div className="sftp">
      <div className="sftp-toolbar">
        <input
          className="sftp-path"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void load(path);
          }}
        />
        <button className="btn" onClick={() => void load(path)}>
          刷新
        </button>
        <button className="btn" onClick={() => void upload()}>
          上传文件
        </button>
        <button className="btn" onClick={mkdir}>
          新建目录
        </button>
        <span className="sftp-title">{title}</span>
      </div>

      {transfer && (
        <div className="sftp-transfer">
          <span>
            {transfer.action === "upload" ? "上传" : "下载"} {transfer.name}：
            {transfer.total > 0 ? `${pct}% (${fmtSize(transfer.done)}/${fmtSize(transfer.total)})` : `${fmtSize(transfer.done)}`}
          </span>
          <div className="sftp-bar">
            <div className="sftp-bar-in" style={{ width: `${transfer.total > 0 ? pct : 100}%` }} />
          </div>
        </div>
      )}

      {err && <div className="sftp-err">{err}</div>}

      <div className="sftp-list">
        <div className="sftp-row sftp-head">
          <span className="c-name">名称</span>
          <span className="c-size">大小</span>
          <span className="c-type">类型</span>
          <span className="c-time">修改时间</span>
          <span className="c-act">操作</span>
        </div>
        {loading ? (
          <div className="sftp-empty">加载中…</div>
        ) : (
          entries.map((e) => (
            <div
              className={`sftp-row ${e.isDir ? "sftp-dir" : ""}`}
              key={e.name}
              onDoubleClick={() => openEntry(e)}
            >
              <span className="c-name" title={e.name}>
                {e.isDir ? "📁 " : "📄 "}
                {e.name}
              </span>
              <span className="c-size">{e.isDir ? "—" : fmtSize(e.size)}</span>
              <span className="c-type">{e.isDir ? "目录" : "文件"}</span>
              <span className="c-time">{new Date(e.modTime).toLocaleString()}</span>
              <span className="c-act">
                {!e.isDir && (
                  <>
                    <button className="btn btn-xs" onClick={() => void download(e)}>
                      下载
                    </button>{" "}
                  </>
                )}
                <button className="btn btn-xs" onClick={() => void doRename(e)}>
                  重命名
                </button>{" "}
                <button className="btn btn-xs btn-danger" onClick={() => void remove(e)}>
                  删除
                </button>
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
