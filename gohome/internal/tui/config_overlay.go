package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

// ConfigOverlay displays the merged configuration and provides
// action items (edit global, edit project, run wizard).
type ConfigOverlay struct {
	ann     config.AnnotatedSettings
	onClose func()
	onEdit  func(scope string)
	actions *SelectListComponent
	maxH    int
}

// NewConfigOverlay creates a ConfigOverlay with the given annotated settings.
// onClose is called when the user presses Esc.
// onEdit is called with the selected action value ("global", "project", or "wizard").
func NewConfigOverlay(ann config.AnnotatedSettings, onClose func(), onEdit func(scope string)) *ConfigOverlay {
	items := []SelectItem{
		{Value: "global", Label: "Edit global config", Description: ann.GlobalPath},
		{Value: "project", Label: "Edit project config", Description: ann.ProjectPath},
		{Value: "wizard", Label: "Run setup wizard", Description: "guided first-time setup"},
	}
	co := &ConfigOverlay{
		ann:     ann,
		onClose: onClose,
		onEdit:  onEdit,
	}
	sl := NewSelectList(items, func(item SelectItem) {
		if co.onEdit != nil {
			co.onEdit(item.Value)
		}
	})
	sl.onCancel = onClose
	co.actions = sl
	return co
}

// SetMaxH sets the maximum height for scrollable content.
func (co *ConfigOverlay) SetMaxH(h int) { co.maxH = h }

// Render returns the terminal lines for the config overlay.
func (co *ConfigOverlay) Render(width int) []string {
	var sb strings.Builder

	sb.WriteString("Configuration\n")
	sb.WriteString(strings.Repeat("-", min(width, 40)) + "\n")

	sb.WriteString("\nModel Configs:\n")
	if len(co.ann.ModelConfig) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		names := make([]string, 0, len(co.ann.ModelConfig))
		for k := range co.ann.ModelConfig {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			mc := co.ann.ModelConfig[name]
			src := co.ann.ModelSources[name]
			if src == "" {
				src = config.SourceDefault
			}
			fmt.Fprintf(&sb, "  %s [%s]\n", name, src)
			fmt.Fprintf(&sb, "    wire: %s  model: %s\n", mc.Wire, mc.ModelName)
			if mc.BaseURL != "" {
				fmt.Fprintf(&sb, "    baseURL: %s\n", mc.BaseURL)
			}
		}
	}

	sb.WriteString("\nSettings:\n")
	writeField := func(name string, value any) {
		src := co.ann.Sources[name]
		if src == "" {
			src = config.SourceDefault
		}
		fmt.Fprintf(&sb, "  %-20s %-12v [%s]\n", name, value, src)
	}

	writeField("defaultModel", co.ann.DefaultModel)
	writeField("shellTimeoutMs", co.ann.ShellTimeoutMs)
	writeField("maxShellTimeoutMs", co.ann.MaxShellTimeoutMs)
	writeField("contextWarnPct", co.ann.ContextWarnPct)
	writeField("contextCritPct", co.ann.ContextCritPct)
	writeField("renderThrottleMs", co.ann.RenderThrottleMs)

	if co.ann.SystemPrompt != "" {
		writeField("systemPrompt", "(set)")
	} else {
		writeField("systemPrompt", "(not set)")
	}

	if len(co.ann.RetryBackoffMs) > 0 {
		writeField("retryBackoffMs", fmt.Sprintf("%v", co.ann.RetryBackoffMs))
	} else {
		writeField("retryBackoffMs", "[]")
	}

	sb.WriteString("\nActions:\n")

	var lines []string
	lines = append(lines, sb.String())
	lines = append(lines, co.actions.Render(width)...)

	return lines
}

// HandleInput delegates keyboard input to the embedded select list.
func (co *ConfigOverlay) HandleInput(msg tea.KeyMsg) tea.Cmd {
	return co.actions.HandleInput(msg)
}

// openConfigOverlayWith creates and displays a ConfigOverlay, storing the
// paths so the editor launch can look them up later.
func (m *Model) openConfigOverlayWith(ann config.AnnotatedSettings) {
	m.configGlobalPath = ann.GlobalPath
	m.configProjectPath = ann.ProjectPath
	m.activeModal = NewConfigOverlay(ann, func() { m.activeModal = nil }, func(scope string) {
		m.configEditScope = scope
		m.activeModal = nil
	})
}
