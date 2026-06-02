package engine

import (
	"sync"
	"time"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// Session 代表了一次持续的人机交互过程。它负责维护该会话的完整历史。
type Session struct {
	ID string
	WorkDir string // 该会话绑定的物理工作区
	CreatedAt time.Time
	UpdatedAt time.Time

	// 存放此 Session 中所有的用户输入、大模型回复和工具调用结果
	history []schema.Message
	mu sync.RWMutex // 读写锁，防止并发读写历史时发生 Data Race
}

func NewSession(id string, workDir string) *Session {
	return &Session{
		ID: id,
		WorkDir: workDir,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		history: make([]schema.Message, 0),
	}
}

// Append 线程安全地向 Session 追加消息
func (s *Session) Append(msgs ...schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msgs...)
	s.UpdatedAt = time.Now()

	// 【持久化预留点】：在真实的工业级实现中（如 Claude Code），
	// 我们会在这将 s.history 以 JSONL 的格式 Append 到 workDir/.claw/sessions/xxx.josnl 中。
	// s.SaveToDisk()
}

// GetWorkingMemory 是驾驭工程的核心
// 它不返回全量历史，而是从后往前截取最近的 N 条消息，形成 Agent 的 “短期记忆”。
func (s *Session) GetWorkingMemory(limit int) []schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.history)
	if total <= limit || limit <= 0 {
		// 如果历史总量小于限制，或不设限，全量返回（需要深拷贝以防外部修改）
		res := make([]schema.Message, total)
		copy(res, s.history)
		return res
	}

	// 截取最近的 limit 条消息
	res := make([]schema.Message, limit)
	copy(res, s.history[total-limit:])

	// 【驾驭防线】：大模型 API 强制要求历史消息的连贯性
	// 如果我们截断的是第一条消息恰好是一个 ToolResult (RoleUser 且含有 ToolCallID)
	// 但发出这个请求的 ToolCall 被我们截断抛弃了，大模型 API 会直接报 400 Bad Request
	// 因此，如果切片首条属于“孤儿”工具响应，我们必须将其强行舍弃，顺延到下一条正常的 User/Assistant 消息。
	for len(res) > 0 {
		if res[0].Role == schema.RoleUser && res[0].ToolCallID != "" {
			res = res[1:]
		} else {
			break
		}
	}

	// 智谱/Anthropic 要求 system 后必须紧跟 user 消息。
	// 多轮工具调用后，截断 working memory 可能丢掉最初的用户 prompt，导致首条变成 assistant → 1214。
	if len(res) > 0 && res[0].Role != schema.RoleUser {
		for _, m := range s.history {
			if m.Role == schema.RoleUser && m.ToolCallID == "" {
				res = append([]schema.Message{m}, res...)
				break
			}
		}
	}

	return res
}

// ===================================
// 全局 Session Manager：用于多用户/多终端隔离
// ===================================

type SessionManager struct {
	sessions map[string]*Session
	mu sync.RWMutex
}

var GlobalSessionMgr = &SessionManager{
	sessions: make(map[string]*Session),
}

// 获取或创建一个会话
func (sm *SessionManager) GetOrCreate(id string, workDir string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sess, exists := sm.sessions[id]; exists {
		return sess
	}
	sess := NewSession(id, workDir)
	sm.sessions[id] = sess

	return sess
}

