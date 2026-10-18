import React, { useEffect, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "@xterm/addon-fit";
import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime";
import { TerminalInput, TerminalResize, CloseTerminal } from "../../wailsjs/go/app/App";
import { useTheme, type Theme } from "../theme";

import "xterm/css/xterm.css";

/** 根据亮/暗主题生成 xterm 主题 */
function xtermTheme(theme: Theme): Record<string, string> {
  return theme === "dark"
    ? {
        background: "#1e1e2e",
        foreground: "#cdd6f4",
        cursor: "#f5e0dc",
        selectionBackground: "#585b70",
        black: "#45475a",
        red: "#f38ba8",
        green: "#a6e3a1",
        yellow: "#f9e2af",
        blue: "#89b4fa",
        magenta: "#cba6f7",
        cyan: "#94e2d5",
        white: "#bac2de",
        brightBlack: "#585b70",
        brightRed: "#f38ba8",
        brightGreen: "#a6e3a1",
        brightYellow: "#f9e2af",
        brightBlue: "#89b4fa",
        brightMagenta: "#cba6f7",
        brightCyan: "#94e2d5",
        brightWhite: "#a6adc8",
      }
    : {
        background: "#ffffff",
        foreground: "#1f2328",
        cursor: "#0969da",
        selectionBackground: "#b6d3f5",
        black: "#24292f",
        red: "#cf222e",
        green: "#116329",
        yellow: "#4d2d00",
        blue: "#0969da",
        magenta: "#8250df",
        cyan: "#1b7c83",
        white: "#6e7781",
        brightBlack: "#57606a",
        brightRed: "#a40e26",
        brightGreen: "#1a7f37",
        brightYellow: "#633c01",
        brightBlue: "#218bff",
        brightMagenta: "#bf3989",
        brightCyan: "#3192aa",
        brightWhite: "#8c959f",
      };
}

interface TerminalViewProps {
  sessionId: string;
  title: string;
  onClosed: (sessionId: string) => void;
}

export default function TerminalView({ sessionId, title, onClosed }: TerminalViewProps) {
  const containerRef = React.useRef<HTMLDivElement>(null);
  const termRef = React.useRef<Terminal | null>(null);
  const fitRef = React.useRef<FitAddon | null>(null);
  const { theme } = useTheme();
  const [connected, setConnected] = useState(true);

  // 初始化 xterm
  useEffect(() => {
    const term = new Terminal({
      fontSize: 13,
      fontFamily: "JetBrains Mono, Fira Code, Consolas, Menlo, monospace",
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      ...xtermTheme(theme),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    termRef.current = term;
    fitRef.current = fit;

    if (containerRef.current) {
      term.open(containerRef.current);
      fit.fit();
    }

    // 用户输入 → 后端
    const inputDisposable = term.onData((data) => {
      void TerminalInput(sessionId, data);
    });

    // 终端尺寸变化 → 后端 PTY
    const doResize = () => {
      try {
        fit.fit();
        const dims = term.cols && term.rows ? { cols: term.cols, rows: term.rows } : null;
        if (dims) void TerminalResize(sessionId, dims.cols, dims.rows);
      } catch {
        /* fit 失败忽略 */
      }
    };
    doResize();
    const ro = new ResizeObserver(doResize);
    if (containerRef.current) ro.observe(containerRef.current);

    // 后端输出 → 终端
    const offOutput = EventsOn("terminal:output", (payload: { id: string; data: string }) => {
      if (payload.id === sessionId) term.write(payload.data);
    });
    // 会话关闭 → 更新 UI 状态
    const offClosed = EventsOn("terminal:closed", (payload: { id: string; reason: string }) => {
      if (payload.id === sessionId) {
        setConnected(false);
        onClosed(sessionId);
      }
    });

    return () => {
      inputDisposable.dispose();
      offOutput();
      offClosed();
      ro.disconnect();
      term.dispose();
      termRef.current = null;
      void CloseTerminal(sessionId).catch(() => {});
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // 主题切换时更新 xterm 配色
  useEffect(() => {
    termRef.current?.options && Object.assign(termRef.current.options, xtermTheme(theme));
  }, [theme]);

  return (
    <div className={`terminal-pane ${connected ? "" : "terminal-disconnected"}`}>
      {!connected && <div className="terminal-overlay">会话已断开{title ? `（${title}）` : ""}</div>}
      <div ref={containerRef} className="terminal-container" />
    </div>
  );
}
