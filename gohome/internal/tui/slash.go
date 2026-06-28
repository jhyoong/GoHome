package tui

import (
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// SlashCallbacks holds optional callbacks for slash commands that require
// external coordination (session management, model switching, etc.).
type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) (string, []common.Message, error)
	CancelSession func(id string)
	SetModel      func(name string) (model string, contextWindow int, err error)
	ListSessions  func() ([]session.Listing, error)
}
