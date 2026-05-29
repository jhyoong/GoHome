package config

// Wire identifies which LLM HTTP protocol an endpoint speaks.
type Wire string

const (
	WireAnthropic Wire = "anthropic"
	WireOpenAI    Wire = "openai"
)

// Endpoint holds connection details for a single LLM endpoint.
type Endpoint struct {
	Wire          Wire              `json:"wire"`
	BaseURL       string            `json:"baseURL"`
	APIKey        string            `json:"apiKey,omitempty"`
	APIKeyEnv     string            `json:"apiKeyEnv,omitempty"`
	DefaultModel  string            `json:"defaultModel"`
	ContextWindow int               `json:"contextWindow,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// Settings is the top-level configuration structure.
type Settings struct {
	Endpoints       map[string]Endpoint `json:"endpoints"`
	DefaultEndpoint string              `json:"defaultEndpoint"`
}
