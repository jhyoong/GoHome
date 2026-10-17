package tui

import (
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// SlashCallbacks holds optional callbacks for slash commands that require
// external coordination (session management, model switching, etc.).
type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) (string, []common.Message, []*session.Session, error)
	CancelSession func(id string) // id is the main session; cancel stops the whole run
	SetModel      func(name string) (model string, contextWindow int, err error)
	ListSessions  func() ([]session.Listing, error)
	OpenConfig    func() (config.AnnotatedSettings, error)
}
