package tui

import "github.com/jhyoong/GoHome/gohome/internal/session"

// SlashCallbacks holds optional callbacks for slash commands that require
// external coordination (session management, model switching, etc.).
type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) error
	CancelSession func(id string)
	SetModel      func(name string) error
	ListSessions  func() ([]session.Listing, error)
}
