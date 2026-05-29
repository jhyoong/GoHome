package common

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockToolUse    BlockKind = "tool_use"
	BlockToolResult BlockKind = "tool_result"
)

type Block struct {
	Kind       BlockKind `json:"kind"`
	Text       string    `json:"text,omitempty"`
	ToolUseID  string    `json:"toolUseId,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	InputJSON  string    `json:"inputJson,omitempty"`
	ResultText string    `json:"resultText,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
}

type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`
}
