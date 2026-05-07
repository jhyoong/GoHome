package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
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
) error {

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

		err = l.llm.Stream(ctx, history, llmTools,
			onToken,
			func(tcs []llm.ToolCall) { toolCalls = tcs; gotToolCalls = true },
			nil,
		)
		if err != nil {
			return fmt.Errorf("LLM stream: %w", err)
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
				l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: "", Approved: false,
				})
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
				l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: result, Approved: true,
				})
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

func toAnySlice(in []map[string]any) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
