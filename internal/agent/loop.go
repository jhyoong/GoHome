package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

type Loop struct {
	llm          *llm.Client
	registry     *tools.Registry
	store        *session.Store
	systemPrompt string
}

func NewLoop(client *llm.Client, reg *tools.Registry, store *session.Store, systemPrompt string) *Loop {
	return &Loop{llm: client, registry: reg, store: store, systemPrompt: systemPrompt}
}

func (l *Loop) Run(ctx context.Context, sessionID, tabID, userMessage string,
	broker *approval.Broker,
	onToken func(string),
	onError func(string),
	onToolResult func(tool, params, result string, approved bool),
	steerCh <-chan string,
	onUsage func(prompt, completion, total int),
	onThinking func(string),
) error {

	if l.store == nil {
		return fmt.Errorf("store is nil")
	}
	if l.llm == nil {
		return fmt.Errorf("llm client is nil")
	}

	msgs, err := l.store.GetMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	if _, err := l.store.AddMessage(ctx, session.Message{
		SessionID: sessionID, Role: "user", Content: userMessage,
	}); err != nil {
		return fmt.Errorf("saving user message: %w", err)
	}

	history := l.buildHistory(msgs, userMessage)
	llmTools := toAnySlice(l.registry.ToLLMTools())

	var finalText string
	var thinkingText string

	for {
		// Drain any steering messages before the next LLM call.
		if steerCh != nil {
		drainSteering:
			for {
				select {
				case steerMsg := <-steerCh:
					if _, err := l.store.AddMessage(ctx, session.Message{
						SessionID: sessionID, Role: "user", Content: steerMsg,
					}); err != nil {
						return fmt.Errorf("saving steering message: %w", err)
					}
					history = append(history, llm.Message{Role: "user", Content: steerMsg})
				default:
					break drainSteering
				}
			}
		}

		var toolCalls []llm.ToolCall
		var gotToolCalls bool
		finalText = ""
		thinkingText = ""

		tokenCollector := func(token string) {
			finalText += token
			onToken(token)
		}
		thinkingCollector := func(token string) {
			thinkingText += token
			if onThinking != nil {
				onThinking(token)
			}
		}

		err = l.llm.Stream(ctx, history, llmTools,
			tokenCollector,
			func(tcs []llm.ToolCall) { toolCalls = tcs; gotToolCalls = true },
			nil,
			onUsage,
			thinkingCollector,
		)
		if err != nil {
			return err
		}

		if !gotToolCalls {
			break
		}

		tcJSON, _ := json.Marshal(toolCalls)
		assistantMsg, err := l.store.AddMessage(ctx, session.Message{
			SessionID: sessionID, Role: "assistant", ToolCalls: string(tcJSON),
		})
		if err != nil {
			return err
		}

		var toolResults []llm.Message
		for _, tc := range toolCalls {
			reqID := uuid.New().String()
			approved, approvalErr := broker.Request(ctx, reqID, tc.Function.Name, json.RawMessage(tc.Function.Arguments))

			var result string
			if approvalErr != nil || !approved {
				result = "denied"
				if approvalErr != nil {
					result = "error: " + approvalErr.Error()
				}
				if _, err := l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: result, Approved: false,
				}); err != nil {
					return fmt.Errorf("saving tool result: %w", err)
				}
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, result, false)
				}
			} else {
				t, ok := l.registry.Get(tc.Function.Name)
				if !ok {
					result = fmt.Sprintf("tool %q not found", tc.Function.Name)
				} else {
					result, err = t.Execute(ctx, json.RawMessage(tc.Function.Arguments))
					if err != nil {
						result = "error: " + err.Error()
						log.Printf("tool %q error: %v", tc.Function.Name, err)
					}
				}
				if _, err := l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: result, Approved: true,
				}); err != nil {
					return fmt.Errorf("saving tool result: %w", err)
				}
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, result, true)
				}
			}

			toolResults = append(toolResults, llm.Message{
				Role: "tool", Content: result, ToolCallID: tc.ID, Name: tc.Function.Name,
			})
		}

		history = append(history, llm.Message{Role: "assistant", ToolCalls: toolCalls})
		history = append(history, toolResults...)
	}

	if finalText != "" {
		if _, err := l.store.AddMessage(ctx, session.Message{
			SessionID: sessionID, Role: "assistant", Content: finalText, Thinking: thinkingText,
		}); err != nil {
			return fmt.Errorf("saving assistant message: %w", err)
		}
	}

	return nil
}

func (l *Loop) buildHistory(msgs []session.Message, newUserMessage string) []llm.Message {
	var history []llm.Message
	if l.systemPrompt != "" {
		history = append(history, llm.Message{Role: "system", Content: l.systemPrompt})
	}
	for _, m := range msgs {
		msg := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCalls != "" {
			json.Unmarshal([]byte(m.ToolCalls), &msg.ToolCalls)
		}
		history = append(history, msg)
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage})
	return history
}

func (l *Loop) GenerateTitle(ctx context.Context, message string) (string, error) {
	var sb strings.Builder
	err := l.llm.Stream(ctx, []llm.Message{
		{
			Role:    "system",
			Content: "Generate a short title of at most 10 words for a conversation starting with the following user message. Reply with only the title, no quotes or trailing punctuation.",
		},
		{Role: "user", Content: message},
	}, nil,
		func(token string) { sb.WriteString(token) },
		func(_ []llm.ToolCall) {},
		nil,
		nil, // onUsage: intentionally nil, title generation does not need token accounting
	)
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(sb.String())
	if title == "" {
		return "", fmt.Errorf("empty title from LLM")
	}
	return title, nil
}

func toAnySlice(in []map[string]any) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
