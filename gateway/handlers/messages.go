package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AnthropicRequest struct {
	Model               string            `json:"model"`
	Messages            []AnthropicMsg    `json:"messages"`
	System              any               `json:"system,omitempty"`
	MaxTokens           *int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int              `json:"max_completion_tokens,omitempty"`
	Temperature         *float64          `json:"temperature,omitempty"`
	TopP                *float64          `json:"top_p,omitempty"`
	Stream              bool              `json:"stream,omitempty"`
	Tools               []any             `json:"tools,omitempty"`
	ToolChoice          any               `json:"tool_choice,omitempty"`
	Metadata            map[string]any    `json:"metadata,omitempty"`
	Extra               map[string]any    `json:"-"`
}

type AnthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type OpenAiRequest struct {
	Model               string         `json:"model"`
	Messages            []OpenAiMsg    `json:"messages"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	Tools               []any          `json:"tools,omitempty"`
	ToolChoice          any            `json:"tool_choice,omitempty"`
}

type OpenAiMsg struct {
	Role       string `json:"role"`
	Content    any    `json:"content,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
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
		Tools:               ar.Tools,
		ToolChoice:          ar.ToolChoice,
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
			var textParts []string
			for _, item := range c {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				itemType, _ := m["type"].(string)
				switch itemType {
				case "text":
					if t, ok := m["text"].(string); ok {
						textParts = append(textParts, t)
					}
				case "tool_result":
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

	return req
}

func MessagesHandler() http.HandlerFunc {
	target := os.Getenv("NEXT_BACKEND_URL")
	if target == "" {
		target = "http://127.0.0.1:20128"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
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

		upstreamURL := target + "/v1/chat/completions"
		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(openAiBytes))
		if err != nil {
			http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
			return
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

		client := &http.Client{
			Timeout: 0,
		}

		resp, err := client.Do(upstreamReq)
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
						Role    string `json:"role"`
						Content string `json:"content"`
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

			text := ""
			stopReason := "end_turn"
			if len(openAiResp.Choices) > 0 {
				text = openAiResp.Choices[0].Message.Content
				if openAiResp.Choices[0].FinishReason == "length" {
					stopReason = "max_tokens"
				}
			}

			out := map[string]any{
				"id":    openAiResp.ID,
				"type":  "message",
				"role":  "assistant",
				"model": anthropicReq.Model,
				"content": []any{
					map[string]any{
						"type": "text",
						"text": text,
					},
				},
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

	var textBlockStarted bool
	var blockIndex int
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
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if !textBlockStarted {
					textBlockStarted = true
					sendSse(w, flusher, "content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": blockIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					})
				}
				outputTokens++
				sendSse(w, flusher, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": choice.Delta.Content,
					},
				})
			}

			if choice.FinishReason != "" {
				if choice.FinishReason == "length" {
					stopReason = "max_tokens"
				} else if choice.FinishReason == "tool_calls" {
					stopReason = "tool_use"
				} else {
					stopReason = "end_turn"
				}
			}
		}
	}

	if textBlockStarted {
		sendSse(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIndex,
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
