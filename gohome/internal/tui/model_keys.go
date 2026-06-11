package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// handleKeyMsg is the top-level key dispatch. It implements a priority cascade:
// Ctrl+C, approval mode, fullscreen overlays, Esc-during-spinner, modal
// components, then normal editing/navigation keys.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1. Ctrl+C double-tap / cancel
	if msg.Type == tea.KeyCtrlC {
		return m.handleCtrlC()
	}

	// 2. Approval mode
	if m.activeApproval != nil {
		return m, m.handleApprovalKey(msg)
	}

	// 3. Fullscreen overlays
	if m.showTokens {
		return m.handleTokensKey(msg)
	}
	if m.showHelp {
		return m.handleHelpKey(msg)
	}

	// 4. Esc-during-spinner
	if msg.Type == tea.KeyEsc && m.spinner.Active() &&
		!m.browsing && !m.selectingModel {
		m.spinner.HandleInput(msg)
		return m, nil
	}

	// 5. Modal components
	if m.browsing && m.sessionBrowser != nil {
		m.sessionBrowser.HandleInput(msg)
		return m, nil
	}
	if m.selectingModel && m.modelSelector != nil {
		m.modelSelector.HandleInput(msg)
		return m, nil
	}

	// 6. Normal mode
	return m.handleNormalKey(msg)
}

// handleCtrlC handles Ctrl+C presses: double-tap quits, single tap cancels
// active approval or in-flight turn, or records tap for future double-tap.
func (m *Model) handleCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()
	doubleTap := now.Sub(m.lastCtrlC) < 500*time.Millisecond
	m.lastCtrlC = now

	if doubleTap {
		return m, tea.Quit
	}

	if m.activeApproval != nil {
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
		m.statusMsg = "Approval dismissed"
		return m, nil
	}

	sv := m.sessions[m.focused]
	if sv != nil && sv.InFlight {
		m.cancelFocusedSessionWith("Cancelled — press Ctrl+C again to quit")
		return m, nil
	}

	m.statusMsg = "Press Ctrl+C again to quit"
	return m, nil
}

// handleNormalKey handles key presses in normal (non-modal) editing mode.
// This covers session focus switching, page scrolling, tab completion,
// enter/submit, external editor, file search navigation, timeline cursor,
// clipboard copy, and editor text input with @-query file search.
func (m *Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg.Type {
	case tea.KeyCtrlCloseBracket:
		m.focusNext()
	case tea.KeyCtrlOpenBracket:
		m.focusPrev()
	case tea.KeyCtrlH:
		m.showHelp = true
		m.helpScroll = 0
		return m, nil
	case tea.KeyPgUp:
		m.chat.ScrollUp(5)
	case tea.KeyPgDown:
		m.chat.ScrollDown(5)
	case tea.KeyTab:
		if m.completeSlash() {
			return m, nil
		}
		if m.confirmFileSearch() {
			return m, nil
		}
	case tea.KeyEnter:
		if msg.Alt {
			m.editor.InsertNewline()
		} else {
			if m.confirmFileSearch() {
				return m, nil
			}
			text := strings.TrimSpace(m.editor.Value())
			if text == "" {
				sv, ok := m.sessions[m.focused]
				if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
					entry := &sv.Timeline[m.cursor]
					if entry.Kind == KindTool || entry.Kind == KindThinking {
						m.chat.DisableAutoScroll(m.winW)
						entry.Expanded = !entry.Expanded
						m.rebuildViewportKeepScroll()
					}
				}
			} else if strings.HasPrefix(text, "/") {
				cmd := m.handleSlashCommand(text)
				m.editor.SetValue("")
				m.rebuildViewport()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else {
				sv := m.getOrCreateSession(m.focused, 0)
				if sv.InFlight {
					if len(m.pendingMessages) >= 10 {
						m.statusMsg = "Message queue full (10)"
					} else {
						m.pendingMessages = append(m.pendingMessages, text)
						m.editor.SetValue("")
					}
				} else {
					sv.Timeline = append(sv.Timeline, TimelineEntry{
						Kind: KindUser,
						Text: text,
					})
					sv.InFlight = true
					m.editor.SetValue("")
					m.statusMsg = ""
					m.cursor = len(sv.Timeline) - 1
					m.rebuildViewport()
					cmds = append(cmds, m.sendInputCmd(text))
				}
			}
		}
		return m, tea.Batch(cmds...)
	case tea.KeyCtrlE:
		return m, m.openExternalEditor()
	default:
		// File search navigation intercepts arrow keys and Esc.
		if m.fileSearching && m.fileSearch.visible {
			if msg.Type == tea.KeyUp {
				m.fileSearch.MoveUp()
				return m, nil
			}
			if msg.Type == tea.KeyDown {
				m.fileSearch.MoveDown()
				return m, nil
			}
			if msg.Type == tea.KeyEsc {
				m.fileSearching = false
				m.fileSearch.Hide()
				return m, nil
			}
		}
		// Timeline cursor navigation when editor is empty.
		if strings.TrimSpace(m.editor.Value()) == "" {
			if keyRune(msg) == 'c' {
				sv, ok := m.sessions[m.focused]
				if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
					text := timelineEntryText(sv.Timeline[m.cursor])
					if err := clipboard.WriteAll(text); err != nil {
						m.statusMsg = fmt.Sprintf("Copy failed: %v", err)
					} else {
						m.statusMsg = "Copied to clipboard"
					}
					return m, nil
				}
			}
			if msg.Type == tea.KeyUp {
				if m.cursor > 0 {
					m.cursor--
				}
				m.rebuildViewportKeepScroll()
				return m, nil
			}
			if msg.Type == tea.KeyDown {
				sv, ok := m.sessions[m.focused]
				if ok && m.cursor < len(sv.Timeline)-1 {
					m.cursor++
				}
				m.rebuildViewportKeepScroll()
				return m, nil
			}
		}
		cmd := m.editor.HandleInput(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Check for @-prefix file search.
		if q, ok := m.extractAtQuery(); ok && q != m.fileSearch.query {
			m.fileSearching = true
			m.fileSearch.query = q
			m.fileSearch.selected = 0
			cmds = append(cmds, searchFilesCmd(q))
		} else if !ok && m.fileSearching {
			m.fileSearching = false
			m.fileSearch.Hide()
		}
	}

	return m, tea.Batch(cmds...)
}

// confirmFileSearch applies the selected file search result to the editor
// and closes the popup. Returns true if a selection was made.
func (m *Model) confirmFileSearch() bool {
	if !m.fileSearching || !m.fileSearch.visible {
		return false
	}
	path := m.fileSearch.SelectedPath()
	if path != "" {
		m.replaceAtQuery(path)
	}
	m.fileSearching = false
	m.fileSearch.Hide()
	return true
}

// extractAtQuery returns the word following the last '@' in the editor when that
// '@' is at the start of the input or preceded by whitespace. Returns ("", false)
// when the pattern is absent or the query contains whitespace.
func (m *Model) extractAtQuery() (string, bool) {
	val := m.editor.Value()
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return "", false
	}
	if idx > 0 {
		prev := val[idx-1]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			return "", false
		}
	}
	query := val[idx+1:]
	if strings.ContainsAny(query, " \t\n") {
		return "", false
	}
	if query == "" {
		return "", false
	}
	return query, true
}

// replaceAtQuery replaces the @<word> fragment in the editor with @replacement
// followed by a trailing space (to prevent re-triggering the search).
func (m *Model) replaceAtQuery(replacement string) {
	val := m.editor.Value()
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return
	}
	newVal := val[:idx] + "@" + replacement + " "
	m.editor.SetValue(newVal)
}
