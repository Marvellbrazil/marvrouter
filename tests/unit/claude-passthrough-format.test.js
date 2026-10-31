import { describe, expect, it, vi } from "vitest";

const { executeMock } = vi.hoisted(() => ({
  executeMock: vi.fn(),
}));

vi.mock("../../open-sse/executors/index.js", () => ({
  getExecutor: () => ({
    noAuth: true,
    execute: executeMock,
  }),
}));

vi.mock("../../open-sse/utils/requestLogger.js", () => ({
  createRequestLogger: async () => ({
    logClientRawRequest: vi.fn(),
    logRawRequest: vi.fn(),
    logTargetRequest: vi.fn(),
    logProviderResponse: vi.fn(),
    logConvertedResponse: vi.fn(),
    logError: vi.fn(),
  }),
}));

vi.mock("@/lib/usageDb.js", () => ({
  trackPendingRequest: vi.fn(),
  appendRequestLog: vi.fn(async () => {}),
  saveRequestDetail: vi.fn(async () => {}),
}));

vi.mock("../../open-sse/handlers/chatCore/streamingHandler.js", () => ({
  handleStreaming: vi.fn(() => new Response("ok")),
  handleStreamingResponse: vi.fn(() => new Response("ok")),
  buildOnStreamComplete: vi.fn(() => vi.fn()),
}));

const { handleChatCore } = await import("../../open-sse/handlers/chatCore.js");
const { prepareClaudeRequest } =
  await import("../../open-sse/translator/formats/claude.js");
const { cloakClaudeTools } =
  await import("../../open-sse/utils/claudeCloaking.js");

describe("Claude Passthrough Format Check", () => {
  it("does not treat an OpenAI-formatted request as native Claude passthrough even if User-Agent is claude-cli", async () => {
    executeMock.mockImplementationOnce(async ({ body }) => ({
      response: { ok: true, status: 200 },
      url: "https://api.anthropic.com/v1/messages",
      headers: {},
      transformedBody: body,
    }));

    const body = {
      model: "claude/claude-sonnet-5-5",
      messages: [{ role: "user", content: "hello" }],
      tools: [
        {
          type: "function",
          function: {
            name: "Bash",
            description: "Run bash command",
            parameters: {
              type: "object",
              properties: { command: { type: "string" } },
            },
          },
        },
      ],
      stream: true,
    };

    await handleChatCore({
      body,
      modelInfo: { provider: "claude", model: "claude-sonnet-5-5" },
      credentials: { apiKey: "sk-ant-api-test" },
      log: { debug: vi.fn(), info: vi.fn(), warn: vi.fn(), line: vi.fn() },
      connectionId: "test-conn",
      rtkEnabled: false,
      headroomEnabled: false,
      cavemanEnabled: false,
      ponytailEnabled: false,
      pxpipeEnabled: false,
      clientRawRequest: {
        endpoint: "/v1/chat/completions",
        body,
        headers: {
          "user-agent": "claude-cli/2.1.280 (external, sdk-cli)",
          "x-app": "cli",
        },
      },
    });

    expect(executeMock).toHaveBeenCalledTimes(1);
    const sentBody = executeMock.mock.calls[0][0].body;

    // The tools sent to Anthropic must NOT have type: "function"
    expect(sentBody.tools[0].type).not.toBe("function");
    // Anthropic tools have name and input_schema
    expect(sentBody.tools[0]).toHaveProperty("name");
    expect(sentBody.tools[0]).toHaveProperty("input_schema");
  });

  it("prepareClaudeRequest normalizes OpenAI tools for provider 'claude'", () => {
    const raw = {
      model: "claude-sonnet-5-5",
      messages: [{ role: "user", content: "hi" }],
      tools: [
        {
          type: "function",
          function: {
            name: "readFile",
            description: "Read a file",
            parameters: {
              type: "object",
              properties: { path: { type: "string" } },
            },
          },
        },
      ],
    };

    const prepared = prepareClaudeRequest(raw, "claude");
    expect(prepared.tools[0].type).toBeUndefined();
    expect(prepared.tools[0].name).toBe("readFile");
    expect(prepared.tools[0].input_schema).toEqual({
      type: "object",
      properties: { path: { type: "string" } },
    });
  });

  it("cloakClaudeTools strips type: 'function' and suffixes client tool", () => {
    const body = {
      tools: [
        {
          type: "function",
          name: "Bash",
          input_schema: { type: "object", properties: {} },
        },
      ],
    };

    const { body: cloaked } = cloakClaudeTools(body);
    const clientTool = cloaked.tools.find((t) => t.name.startsWith("Bash"));
    expect(clientTool).toBeDefined();
    expect(clientTool.type).toBeUndefined();
  });
});
