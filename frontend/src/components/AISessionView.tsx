import React, { useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime";
import { AuditHistory } from "../../wailsjs/go/app/App";
import TerminalView from "./TerminalView";

/** AI 审计条目（与后端 audit.Entry 一致） */
interface AuditItem {
  time: string;
  source: string;
  sessionId: string;
  tool: string;
  args?: Record<string, unknown>;
  result: string;
  durationMs: number;
}

/** 单条 AI 操作记录的时间线条目 */
function AuditRow({ item }: { item: AuditItem }) {
  const time = new Date(item.time).toLocaleTimeString("zh-CN", { hour12: false });
  const cmd = typeof item.args?.command === "string" ? item.args.command : "";
  const ok = !item.result.startsWith("err") && !item.result.includes("失败");

  return (
    <div className={`audit-row ${ok ? "" : "audit-row-err"}`}>
      <div className="audit-head">
        <span className="audit-tool">{item.tool}</span>
        <span className="audit-time">{time}</span>
        <span className="audit-ms">{item.durationMs}ms</span>
      </div>
      {cmd && <div className="audit-cmd">{cmd}</div>}
      <div className="audit-result">{item.result}</div>
    </div>
  );
}

interface AISessionViewProps {
  sessionId: string;
  title: string;
}

/** AI 共享会话：左侧可交互终端（AI 与用户共用），右侧 AI 操作记录时间线 */
export default function AISessionView({ sessionId, title }: AISessionViewProps) {
  const [audits, setAudits] = useState<AuditItem[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  // 初始加载历史 + 实时订阅本会话审计
  useEffect(() => {
    let mounted = true;
    void AuditHistory(200).then((list) => {
      if (!mounted) return;
      const mine = (list as AuditItem[]).filter((x) => x.sessionId === sessionId);
      setAudits(mine);
    });

    const off = EventsOn("ai:audit", (e: AuditItem) => {
      if (e.sessionId === sessionId) {
        setAudits((prev) => [...prev, e]);
      }
    });
    return () => {
      mounted = false;
      off();
    };
  }, [sessionId]);

  // 新记录自动滚到底
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [audits.length]);

  return (
    <div className="ai-session">
      <div className="ai-terminal">
        <TerminalView sessionId={sessionId} title={title} onClosed={() => {}} />
      </div>
      <aside className="ai-audit">
        <div className="ai-audit-header">AI 操作记录</div>
        <div className="ai-audit-list" ref={scrollRef}>
          {audits.length === 0 && <div className="ai-audit-empty">暂无 AI 操作</div>}
          {audits.map((a, i) => (
            <AuditRow key={i} item={a} />
          ))}
        </div>
      </aside>
    </div>
  );
}
