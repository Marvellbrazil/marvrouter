package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AnthropicRequest struct {
	Model               string         `json:"model"`
	Messages            []AnthropicMsg `json:"messages"`
	System              any            `json:"system,omitempty"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	Tools               []any          `json:"tools,omitempty"`
	ToolChoice          any            `json:"tool_choice,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type AnthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type OpenAiRequest struct {
	Model               string      `json:"model"`
	Messages            []OpenAiMsg `json:"messages"`
	MaxTokens           *int        `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int        `json:"max_completion_tokens,omitempty"`
	Temperature         *float64    `json:"temperature,omitempty"`
	TopP                *float64    `json:"top_p,omitempty"`
	Stream              bool        `json:"stream"`
	Tools               []any       `json:"tools,omitempty"`
	ToolChoice          any         `json:"tool_choice,omitempty"`
}

type OpenAiMsg struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func parseSystemPrompt(sys any) string {
	if sys == nil {
		return ""
	}
	switch v := sys.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for i, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					if i > 0 {
						sb.WriteString("\n\n")
					}
					sb.WriteString(t)
				}
			} else if s, ok := item.(string); ok {
				if i > 0 {
					sb.WriteString("\n\n")
				}
				sb.WriteString(s)
			}
		}
		return sb.String()
	}
	return ""
}

func convertAnthropicToOpenAi(ar *AnthropicRequest) *OpenAiRequest {
	req := &OpenAiRequest{
		Model:               ar.Model,
		MaxTokens:           ar.MaxTokens,
		MaxCompletionTokens: ar.MaxCompletionTokens,
		Temperature:         ar.Temperature,
		TopP:                ar.TopP,
		Stream:              ar.Stream,
	}

	sysPrompt := parseSystemPrompt(ar.System)
	if sysPrompt != "" {
		req.Messages = append(req.Messages, OpenAiMsg{
			Role:    "system",
			Content: sysPrompt,
		})
	}

	for _, msg := range ar.Messages {
		switch c := msg.Content.(type) {
		case string:
			req.Messages = append(req.Messages, OpenAiMsg{
				Role:    msg.Role,
				Content: c,
			})
		case []any:
			if msg.Role == "assistant" {
				var textContent string
				var toolCalls []ToolCall
				for _, item := range c {
					m, ok := item.(map[string]any)
					if !ok {
						continue
					}
					itemType, _ := m["type"].(string)
					if itemType == "text" {
						if t, ok := m["text"].(string); ok {
							if textContent != "" {
								textContent += "\n"
							}
							textContent += t
						}
					} else if itemType == "tool_use" {
						id, _ := m["id"].(string)
						name, _ := m["name"].(string)
						var argsStr string
						if rawInput, ok := m["input"].(string); ok {
							argsStr = rawInput
						} else if m["input"] != nil {
							b, _ := json.Marshal(m["input"])
							argsStr = string(b)
						} else {
							argsStr = "{}"
						}
						toolCalls = append(toolCalls, ToolCall{
							ID:   id,
							Type: "function",
							Function: FunctionCall{
								Name:      name,
								Arguments: argsStr,
							},
						})
					}
				}
				var finalContent any = textContent
				if textContent == "" && len(toolCalls) > 0 {
					finalContent = nil
				}
				req.Messages = append(req.Messages, OpenAiMsg{
					Role:      "assistant",
					Content:   finalContent,
					ToolCalls: toolCalls,
				})
			} else {
				var textParts []string
				for _, item := range c {
					m, ok := item.(map[string]any)
					if !ok {
						continue
					}
					itemType, _ := m["type"].(string)
					if itemType == "text" {
						if t, ok := m["text"].(string); ok {
							textParts = append(textParts, t)
						}
					} else if itemType == "tool_result" {
						toolUseId, _ := m["tool_use_id"].(string)
						contentStr := ""
						if rc, ok := m["content"].(string); ok {
							contentStr = rc
						} else if rcArr, ok := m["content"].([]any); ok {
							var sb strings.Builder
							for _, rci := range rcArr {
								if rcm, ok := rci.(map[string]any); ok {
									if rct, ok := rcm["text"].(string); ok {
										sb.WriteString(rct)
									}
								}
							}
							contentStr = sb.String()
						} else if m["content"] != nil {
							b, _ := json.Marshal(m["content"])
							contentStr = string(b)
						}
						req.Messages = append(req.Messages, OpenAiMsg{
							Role:       "tool",
							ToolCallID: toolUseId,
							Content:    contentStr,
						})
					}
				}
				if len(textParts) > 0 {
					req.Messages = append(req.Messages, OpenAiMsg{
						Role:    msg.Role,
						Content: strings.Join(textParts, "\n\n"),
					})
				}
			}
		}
	}

	if len(ar.Tools) > 0 {
		for _, t := range ar.Tools {
			if tm, ok := t.(map[string]any); ok {
				name, _ := tm["name"].(string)
				description, _ := tm["description"].(string)
				params := tm["input_schema"]
				if params == nil {
					params = map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					}
				}
				req.Tools = append(req.Tools, map[string]any{
					"type": "function",
					"function": map[string]any{
						"name":        name,
						"description": description,
						"parameters":  params,
					},
				})
			}
		}

		if ar.ToolChoice != nil {
			if tcStr, ok := ar.ToolChoice.(string); ok {
				req.ToolChoice = tcStr
			} else if tcMap, ok := ar.ToolChoice.(map[string]any); ok {
				tcType, _ := tcMap["type"].(string)
				switch tcType {
				case "auto":
					req.ToolChoice = "auto"
				case "any":
					req.ToolChoice = "required"
				case "tool":
					if tcName, ok := tcMap["name"].(string); ok {
						req.ToolChoice = map[string]any{
							"type": "function",
							"function": map[string]any{
								"name": tcName,
							},
						}
					}
				}
			}
		}
	}

	return req
}

func MessagesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		primary := os.Getenv("NEXT_BACKEND_URL")
		if primary == "" {
			primary = "http://127.0.0.1:20128"
		}
		fallback := "http://127.0.0.1:20128"
		if primary == fallback {
			fallback = "http://127.0.0.1:20127"
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		var anthropicReq AnthropicRequest
		if err := json.Unmarshal(bodyBytes, &anthropicReq); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		openAiReq := convertAnthropicToOpenAi(&anthropicReq)
		openAiBytes, err := json.Marshal(openAiReq)
		if err != nil {
			http.Error(w, "Failed to translate request", http.StatusInternalServerError)
			return
		}

		makeRequest := func(targetHost string) (*http.Response, error) {
			upstreamURL := targetHost + "/v1/chat/completions"
			upstreamReq, reqErr := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(openAiBytes))
			if reqErr != nil {
				return nil, reqErr
			}

			for k, v := range r.Header {
				lower := strings.ToLower(k)
				if lower == "content-length" || lower == "host" || lower == "accept-encoding" {
					continue
				}
				upstreamReq.Header[k] = v
			}
			upstreamReq.Header.Set("Content-Type", "application/json")

			apiKey := r.Header.Get("x-api-key")
			if apiKey != "" && upstreamReq.Header.Get("Authorization") == "" {
				upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
			}

			client := &http.Client{Timeout: 0}
			return client.Do(upstreamReq)
		}

		resp, err := makeRequest(primary)
		if err != nil && primary != fallback && fallback != "" {
			resp, err = makeRequest(fallback)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Upstream request failed: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			for k, v := range resp.Header {
				w.Header()[k] = v
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		if !anthropicReq.Stream {
			var openAiResp struct {
				ID      string `json:"id"`
				Model   string `json:"model"`
				Choices []struct {
					Message struct {
						Role      string     `json:"role"`
						Content   string     `json:"content"`
						ToolCalls []ToolCall `json:"tool_calls"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&openAiResp); err != nil {
				http.Error(w, "Failed to decode upstream JSON", http.StatusBadGateway)
				return
			}

			var contentBlocks []any
			stopReason := "end_turn"

			if len(openAiResp.Choices) > 0 {
				choice := openAiResp.Choices[0]
				if choice.Message.Content != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": choice.Message.Content,
					})
				}
				for _, tc := range choice.Message.ToolCalls {
					var parsedInput any
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsedInput); err != nil {
						parsedInput = map[string]any{"raw": tc.Function.Arguments}
					}
					contentBlocks = append(contentBlocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": parsedInput,
					})
				}
				if choice.FinishReason == "tool_calls" || choice.FinishReason == "function_call" {
					stopReason = "tool_use"
				} else if choice.FinishReason == "length" {
					stopReason = "max_tokens"
				}
			}

			out := map[string]any{
				"id":            openAiResp.ID,
				"type":          "message",
				"role":          "assistant",
				"model":         anthropicReq.Model,
				"content":       contentBlocks,
				"stop_reason":   stopReason,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  openAiResp.Usage.PromptTokens,
					"output_tokens": openAiResp.Usage.CompletionTokens,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("x-shim", "9router-go-adapter")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		streamOpenAiToAnthropic(r.Context(), resp.Body, w, flusher, anthropicReq.Model)
	}
}

func sendSse(w io.Writer, flusher http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	flusher.Flush()
}

func streamOpenAiToAnthropic(ctx context.Context, upstream io.Reader, w http.ResponseWriter, flusher http.Flusher, modelName string) {
	msgId := "msg_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	sendSse(w, flusher, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgId,
			"type":          "message",
			"role":          "assistant",
			"model":         modelName,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	})

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = w.Write([]byte("event: ping\ndata: {\"type\":\"ping\"}\n\n"))
				flusher.Flush()
			}
		}
	}()

	scanner := bufio.NewScanner(upstream)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var currentBlockType string
	var currentBlockIndex = -1
	var currentToolCallID string
	var outputTokens int
	stopReason := "end_turn"

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(dataStr) == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if currentBlockType != "text" {
					if currentBlockType != "" {
						sendSse(w, flusher, "content_block_stop", map[string]any{
							"type":  "content_block_stop",
							"index": currentBlockIndex,
						})
					}
					currentBlockIndex++
					currentBlockType = "text"
					sendSse(w, flusher, "content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": currentBlockIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					})
				}
				outputTokens++
				sendSse(w, flusher, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": currentBlockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": choice.Delta.Content,
					},
				})
			}

			if len(choice.Delta.ToolCalls) > 0 {
				for _, tc := range choice.Delta.ToolCalls {
					tcID := tc.ID
					if tcID == "" {
						tcID = currentToolCallID
					}
					if tcID == "" {
						b := make([]byte, 4)
						_, _ = rand.Read(b)
						tcID = "call_" + hex.EncodeToString(b)
					}
					fnName := tc.Function.Name

					if currentBlockType != "tool_use" || (tc.ID != "" && tc.ID != currentToolCallID) {
						if currentBlockType != "" {
							sendSse(w, flusher, "content_block_stop", map[string]any{
								"type":  "content_block_stop",
								"index": currentBlockIndex,
							})
						}
						currentBlockIndex++
						currentBlockType = "tool_use"
						currentToolCallID = tcID
						sendSse(w, flusher, "content_block_start", map[string]any{
							"type":  "content_block_start",
							"index": currentBlockIndex,
							"content_block": map[string]any{
								"type":  "tool_use",
								"id":    tcID,
								"name":  fnName,
								"input": map[string]any{},
							},
						})
					}

					if tc.Function.Arguments != "" {
						outputTokens++
						sendSse(w, flusher, "content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": currentBlockIndex,
							"delta": map[string]any{
								"type":         "input_json_delta",
								"partial_json": tc.Function.Arguments,
							},
						})
					}
				}
			}

			if choice.FinishReason != "" {
				if choice.FinishReason == "tool_calls" || choice.FinishReason == "function_call" {
					stopReason = "tool_use"
				} else if choice.FinishReason == "length" {
					stopReason = "max_tokens"
				} else {
					stopReason = "end_turn"
				}
			}
		}
	}

	if currentBlockType != "" {
		sendSse(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": currentBlockIndex,
		})
	}

	sendSse(w, flusher, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})

	sendSse(w, flusher, "message_stop", map[string]any{
		"type": "message_stop",
	})
}

func NewCatchAllReverseProxy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		primary := os.Getenv("NEXT_BACKEND_URL")
		if primary == "" {
			primary = "http://127.0.0.1:20128"
		}
		fallback := "http://127.0.0.1:20128"
		if primary == fallback {
			fallback = "http://127.0.0.1:20127"
		}

		targetURL, _ := url.Parse(primary)
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			if fallback != "" && primary != fallback {
				fbURL, _ := url.Parse(fallback)
				fbProxy := httputil.NewSingleHostReverseProxy(fbURL)
				fbProxy.ServeHTTP(rw, req)
				return
			}
			http.Error(rw, "Backend unavailable", http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	}
}
