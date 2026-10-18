// Package app 提供 Wails 前端可调用的绑定方法：
//   - 连接配置管理（增删改查）
//   - SSH 终端会话（打开 / 输入 / 缩放 / 关闭）
//   - 会话输出与状态通过 Wails 事件推送给前端
package app

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sakishell/audit"
	"sakishell/mcp"
	"sakishell/sshx"
	"sakishell/store"
)

// App 是 Wails 绑定到前端的主服务结构体
type App struct {
	ctx       context.Context
	store     *store.Store
	pool      *sshx.Pool
	hostKeys  *sshx.HostKeyStore
	mcpServer *mcp.Server // GUI 嵌入模式的 MCP Server（headless 子命令另行创建）
}

// NewApp 构造 App（连接配置存储与主机密钥存储共用同一数据目录）
func NewApp(dataDir string) (*App, error) {
	st, err := store.New(dataDir)
	if err != nil {
		return nil, fmt.Errorf("初始化配置存储失败: %w", err)
	}
	hk, err := sshx.NewHostKeyStore(st.DataDir())
	if err != nil {
		return nil, fmt.Errorf("初始化主机密钥存储失败: %w", err)
	}
	a := &App{
		store:    st,
		pool:     sshx.NewPool(),
		hostKeys: hk,
	}

	// GUI 嵌入模式 MCP Server：共享会话池与审计，接线前端事件
	ms, err := mcp.NewServer(dataDir)
	if err != nil {
		return nil, fmt.Errorf("初始化 MCP Server 失败: %w", err)
	}
	ms.UsePool(a.pool)
	ms.OnSessionOpened = func(info mcp.AIOpenInfo) {
		a.emit(EvAISessionOpened, map[string]any{"id": info.SessionID, "hostPort": info.HostPort, "title": info.Title})
	}
	ms.OnData = func(sessionID string, data []byte) {
		a.emit(EvTerminalOutput, map[string]any{"id": sessionID, "data": string(data)})
	}
	ms.Audit().OnLog = func(e audit.Entry) {
		a.emit(EvAIAudit, e)
	}
	a.mcpServer = ms
	return a, nil
}

// Startup Wails 生命周期回调：保存 context
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

// Shutdown Wails 生命周期回调：关闭全部会话
func (a *App) Shutdown(ctx context.Context) {
	a.pool.CloseAll()
}

// emit 向所有前端窗口广播事件
func (a *App) emit(event string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data)
	}
}

// ---------- 连接配置管理 ----------

// ListConnections 返回全部连接配置
func (a *App) ListConnections() ([]store.Connection, error) {
	return a.store.List()
}

// SaveConnection 新增或更新连接配置，返回保存后的配置
func (a *App) SaveConnection(c store.Connection) (store.Connection, error) {
	return a.store.Save(c)
}

// DeleteConnection 删除连接配置
func (a *App) DeleteConnection(id string) error {
	return a.store.Delete(id)
}

// ListTrustedHosts 返回已信任的主机指纹列表（用于"忘记主机"管理）
func (a *App) ListTrustedHosts() map[string]string {
	return a.hostKeys.List()
}

// ForgetHost 移除指定主机的信任记录
func (a *App) ForgetHost(hostPort string) error {
	return a.hostKeys.Reset(hostPort)
}
