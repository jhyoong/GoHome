package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

type wizardStep int

const (
	wizardStepWire wizardStep = iota
	wizardStepBaseURL
	wizardStepKeySource
	wizardStepKeyValue
	wizardStepModelName
	wizardStepConfigName
	wizardStepSudoCache
	wizardStepConfirm
	wizardStepDenylistOffer
	wizardStepDenylistDone
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
	sudoCache  bool

	selectList *SelectListComponent
	textBuf    string
	prompt     string
	errMsg     string // shown under the prompt when a write fails
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
	w.errMsg = ""

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

	case wizardStepSudoCache:
		w.prompt = "Cache sudo password in memory for the session?"
		items := []SelectItem{
			{Value: "no", Label: "No", Description: "ask for the password on every sudo command"},
			{Value: "yes", Label: "Yes", Description: "ask once, reuse until gohome exits"},
		}
		w.selectList = NewSelectList(items, func(item SelectItem) {
			w.sudoCache = item.Value == "yes"
			w.step++
			w.buildStep()
		})
		w.selectList.onCancel = func() {
			w.step--
			w.buildStep()
		}

	case wizardStepConfirm:
		w.rebuildConfirmStep()

	case wizardStepDenylistOffer:
		w.prompt = "Would you like to configure a shell command denylist?"
		items := []SelectItem{
			{Value: "yes", Label: "Yes", Description: "create ~/.gohome/denylist.json with default patterns"},
			{Value: "no", Label: "No", Description: "skip denylist setup"},
		}
		w.selectList = NewSelectList(items, func(item SelectItem) {
			if item.Value == "yes" {
				if err := w.scaffoldDenylist(); err != nil {
					w.errMsg = "Denylist setup failed: " + err.Error()
					return
				}
			}
			w.step = wizardStepDenylistDone
			w.buildStep()
		})
		w.selectList.onCancel = func() {
			w.step = wizardStepDenylistDone
			w.buildStep()
		}

	case wizardStepDenylistDone:
		w.onCancel()
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
			if err := w.save(); err != nil {
				w.errMsg = "Save failed: " + err.Error()
				return
			}
			w.step = wizardStepDenylistOffer
			w.buildStep()
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
	fmt.Fprintf(&sb, "  Config name:  %s\n", w.configName)
	fmt.Fprintf(&sb, "  Wire format:  %s\n", w.wire)
	fmt.Fprintf(&sb, "  Base URL:     %s\n", w.baseURL)
	if w.keySource == "env" {
		fmt.Fprintf(&sb, "  API key env:  %s\n", w.keyValue)
	} else {
		fmt.Fprintf(&sb, "  API key:      %s\n", w.keyValue)
	}
	fmt.Fprintf(&sb, "  Model name:   %s\n", w.modelName)
	sudo := "off"
	if w.sudoCache {
		sudo = "on"
	}
	fmt.Fprintf(&sb, "  Sudo cache:   %s\n", sudo)
	return sb.String()
}

func (w *ConfigWizard) save() error {
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
		ModelConfig:       map[string]config.ModelConfig{w.configName: mc},
		DefaultModel:      w.configName,
		CacheSudoPassword: w.sudoCache,
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Owner-only: the file may hold a literal API key. Perms apply only to
	// newly created files and folders; existing ones keep their mode.
	if err := os.MkdirAll(filepath.Dir(w.outputPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(w.outputPath, data, 0o600); err != nil {
		return err
	}

	if w.onSave != nil {
		w.onSave(w.outputPath)
	}
	return nil
}

func (w *ConfigWizard) scaffoldDenylist() error {
	path := filepath.Join(filepath.Dir(w.outputPath), "denylist.json")
	// Encode without HTML escaping so "&" stays readable in the file.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(guard.DenylistFile{Shell: guard.DefaultDenyPatterns}); err != nil {
		return err
	}
	content := buf.Bytes()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

// Render returns the wizard UI as lines of text.
func (w *ConfigWizard) Render(width int) []string {
	var lines []string
	stepNum := int(w.step) + 1
	lines = append(lines, fmt.Sprintf("Setup Wizard (step %d/10)", stepNum))
	lines = append(lines, "")
	lines = append(lines, w.prompt)
	if w.errMsg != "" {
		lines = append(lines, w.errMsg)
	}

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
			_, size := utf8.DecodeLastRuneInString(w.textBuf)
			w.textBuf = w.textBuf[:len(w.textBuf)-size]
		}
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r != 0 {
				w.textBuf += string(r)
			}
		}
	}
	return nil
}
