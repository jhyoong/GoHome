package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

type wizardStep int

const (
	wizardStepWire       wizardStep = iota
	wizardStepBaseURL
	wizardStepKeySource
	wizardStepKeyValue
	wizardStepModelName
	wizardStepConfigName
	wizardStepConfirm
)

// ConfigWizard walks the user through creating a new model configuration.
type ConfigWizard struct {
	step       wizardStep
	onCancel   func()
	onSave     func(path string)
	outputPath string

	wire       string
	baseURL    string
	keySource  string // "env" or "literal"
	keyValue   string
	modelName  string
	configName string

	selectList *SelectListComponent
	textBuf    string
	prompt     string
}

// NewConfigWizard creates a wizard starting at the wire-format selection step.
func NewConfigWizard(onCancel func(), onSave func(path string)) *ConfigWizard {
	w := &ConfigWizard{
		onCancel: onCancel,
		onSave:   onSave,
	}
	home, err := os.UserHomeDir()
	if err == nil {
		w.outputPath = filepath.Join(home, ".gohome", "settings.json")
	}
	w.buildStep()
	return w
}

func (w *ConfigWizard) buildStep() {
	w.selectList = nil
	w.textBuf = ""

	switch w.step {
	case wizardStepWire:
		w.prompt = "Select wire format:"
		items := []SelectItem{
			{Value: "anthropic", Label: "anthropic", Description: "Anthropic API format"},
			{Value: "openai", Label: "openai", Description: "OpenAI-compatible format"},
		}
		w.selectList = NewSelectList(items, func(item SelectItem) {
			w.wire = item.Value
			w.step++
			w.buildStep()
		})
		w.selectList.onCancel = w.onCancel

	case wizardStepBaseURL:
		w.prompt = "Enter base URL (e.g. https://api.anthropic.com):"

	case wizardStepKeySource:
		w.prompt = "How is your API key provided?"
		items := []SelectItem{
			{Value: "env", Label: "Environment variable", Description: "read from an env var at runtime"},
			{Value: "literal", Label: "Literal value", Description: "store the key directly in config"},
		}
		w.selectList = NewSelectList(items, func(item SelectItem) {
			w.keySource = item.Value
			w.step++
			w.buildStep()
		})
		w.selectList.onCancel = func() {
			w.step--
			w.buildStep()
		}

	case wizardStepKeyValue:
		if w.keySource == "env" {
			w.prompt = "Enter environment variable name (e.g. ANTHROPIC_API_KEY):"
		} else {
			w.prompt = "Enter API key:"
		}

	case wizardStepModelName:
		w.prompt = "Enter model name (e.g. claude-sonnet-4-20250514):"

	case wizardStepConfigName:
		w.prompt = "Enter config name (key in modelConfig map, e.g. claude):"

	case wizardStepConfirm:
		w.rebuildConfirmStep()
	}
}

func (w *ConfigWizard) rebuildConfirmStep() {
	w.prompt = w.summaryText()
	items := []SelectItem{
		{Value: "save", Label: "Save", Description: "write " + w.outputPath},
		{Value: "cancel", Label: "Cancel", Description: "discard and go back"},
	}
	w.selectList = NewSelectList(items, func(item SelectItem) {
		if item.Value == "save" {
			w.save()
		} else {
			w.onCancel()
		}
	})
	w.selectList.onCancel = func() {
		w.step--
		w.buildStep()
	}
}

func (w *ConfigWizard) summaryText() string {
	var sb strings.Builder
	sb.WriteString("Setup summary:\n")
	sb.WriteString(fmt.Sprintf("  Config name:  %s\n", w.configName))
	sb.WriteString(fmt.Sprintf("  Wire format:  %s\n", w.wire))
	sb.WriteString(fmt.Sprintf("  Base URL:     %s\n", w.baseURL))
	if w.keySource == "env" {
		sb.WriteString(fmt.Sprintf("  API key env:  %s\n", w.keyValue))
	} else {
		sb.WriteString(fmt.Sprintf("  API key:      %s\n", w.keyValue))
	}
	sb.WriteString(fmt.Sprintf("  Model name:   %s\n", w.modelName))
	return sb.String()
}

func (w *ConfigWizard) save() {
	mc := config.ModelConfig{
		Wire:      config.Wire(w.wire),
		BaseURL:   w.baseURL,
		ModelName: w.modelName,
	}
	if w.keySource == "env" {
		mc.APIKeyEnv = w.keyValue
	} else {
		mc.APIKey = w.keyValue
	}

	s := config.Settings{
		ModelConfig:  map[string]config.ModelConfig{w.configName: mc},
		DefaultModel: w.configName,
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}

	if err := os.MkdirAll(filepath.Dir(w.outputPath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(w.outputPath, data, 0o644); err != nil {
		return
	}

	if w.onSave != nil {
		w.onSave(w.outputPath)
	}
}

// Render returns the wizard UI as lines of text.
func (w *ConfigWizard) Render(width int) []string {
	var lines []string
	stepNum := int(w.step) + 1
	lines = append(lines, fmt.Sprintf("Setup Wizard (step %d/7)", stepNum))
	lines = append(lines, "")
	lines = append(lines, w.prompt)

	if w.selectList != nil {
		lines = append(lines, w.selectList.Render(width)...)
	} else {
		lines = append(lines, "> "+w.textBuf+"\x1b[7m \x1b[0m")
		lines = append(lines, "Enter to continue, Esc to go back")
	}
	return lines
}

// HandleInput processes a key event for the current wizard step.
func (w *ConfigWizard) HandleInput(msg tea.KeyMsg) tea.Cmd {
	if w.selectList != nil {
		return w.selectList.HandleInput(msg)
	}

	switch msg.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(w.textBuf)
		if val == "" {
			return nil
		}
		switch w.step {
		case wizardStepBaseURL:
			w.baseURL = val
		case wizardStepKeyValue:
			w.keyValue = val
		case wizardStepModelName:
			w.modelName = val
		case wizardStepConfigName:
			w.configName = val
		}
		w.step++
		w.buildStep()
	case tea.KeyEsc:
		if w.step == wizardStepWire {
			w.onCancel()
		} else {
			w.step--
			w.buildStep()
		}
	case tea.KeyBackspace:
		if len(w.textBuf) > 0 {
			w.textBuf = w.textBuf[:len(w.textBuf)-1]
		}
	case tea.KeyRunes:
		w.textBuf += string(msg.Runes)
	}
	return nil
}
