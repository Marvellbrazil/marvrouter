package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testResponseRecorder struct {
	*httptest.ResponseRecorder
}

func (m *testResponseRecorder) Flush() {}

func TestConvertAnthropicToOpenAiWithTools(t *testing.T) {
	ar := &AnthropicRequest{
		Model: "claude-3-5-sonnet",
		System: []any{
			map[string]any{"type": "text", "text": "You are a helpful assistant."},
		},
		Messages: []AnthropicMsg{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "Call tool"},
				},
			},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_123",
						"name":  "get_weather",
						"input": map[string]any{"city": "Jakarta"},
					},
				},
			},
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":         "tool_result",
						"tool_use_id":  "toolu_123",
						"content":      "Sunny, 32C",
					},
				},
			},
		},
		Tools: []any{
			map[string]any{
				"name":        "get_weather",
				"description": "Get current weather",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
		},
		ToolChoice: map[string]any{"type": "auto"},
	}

	req := convertAnthropicToOpenAi(ar)

	if req.Model != "claude-3-5-sonnet" {
		t.Fatalf("expected model claude-3-5-sonnet, got %s", req.Model)
	}

	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages (sys + user + assistant + tool), got %d", len(req.Messages))
	}

	if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are a helpful assistant." {
		t.Fatalf("system message mismatch: %+v", req.Messages[0])
	}

	asstMsg := req.Messages[2]
	if asstMsg.Role != "assistant" || len(asstMsg.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls mismatch: %+v", asstMsg)
	}
	if asstMsg.ToolCalls[0].ID != "toolu_123" || asstMsg.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool call mismatch: %+v", asstMsg.ToolCalls[0])
	}

	toolResultMsg := req.Messages[3]
	if toolResultMsg.Role != "tool" || toolResultMsg.ToolCallID != "toolu_123" || toolResultMsg.Content != "Sunny, 32C" {
		t.Fatalf("tool result mismatch: %+v", toolResultMsg)
	}

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.ToolChoice != "auto" {
		t.Fatalf("expected tool_choice auto, got %v", req.ToolChoice)
	}
}

func TestStreamOpenAiToAnthropicToolCalls(t *testing.T) {
	rawSse := `data: {"id":"chatcmpl-abc","model":"gpt-4","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_01","type":"function","function":{"name":"run_bash","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","model":"gpt-4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","model":"gpt-4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"ls\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","model":"gpt-4","choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

	rec := &testResponseRecorder{httptest.NewRecorder()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamOpenAiToAnthropic(ctx, strings.NewReader(rawSse), rec, rec, "claude-3-5-sonnet")

	body := rec.Body.String()

	if !strings.Contains(body, "event: content_block_start") {
		t.Fatalf("expected content_block_start, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"tool_use"`) {
		t.Fatalf("expected tool_use block start, got:\n%s", body)
	}
	if !strings.Contains(body, `"name":"run_bash"`) {
		t.Fatalf("expected run_bash tool name, got:\n%s", body)
	}
	if !strings.Contains(body, "input_json_delta") {
		t.Fatalf("expected input_json_delta, got:\n%s", body)
	}
	if !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected stop_reason tool_use, got:\n%s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("expected message_stop event, got:\n%s", body)
	}
}

func TestConvertAnthropicToOpenAiSendsExplicitStreamFlag(t *testing.T) {
	cases := []struct {
		name   string
		stream bool
		want   string
	}{
		{"non-stream", false, `"stream":false`},
		{"stream", true, `"stream":true`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ar := &AnthropicRequest{
				Model:    "claude-sonnet-5",
				Stream:   tc.stream,
				Messages: []AnthropicMsg{{Role: "user", Content: "hi"}},
			}
			body, err := json.Marshal(convertAnthropicToOpenAi(ar))
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("expected %s in %s", tc.want, body)
			}
		})
	}
}

func TestCatchAllReverseProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dashboard-ok"))
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	t.Setenv("NEXT_BACKEND_URL", backend.URL)

	proxy := NewCatchAllReverseProxy()

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "dashboard-ok" {
		t.Fatalf("expected dashboard-ok, got %s", rec.Body.String())
	}
}
