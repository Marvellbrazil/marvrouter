import { describe, expect, it, vi, beforeEach } from "vitest";

const mockConnections = [
  {
    id: "conn-1",
    provider: "claude",
    name: "Claude Account",
    email: "claude@example.com",
    authType: "oauth",
    accessToken: "test-token",
  },
  {
    id: "conn-2",
    provider: "cursor",
    name: "Cursor Pro",
    email: "cursor@example.com",
    authType: "oauth",
    accessToken: "cursor-token",
  },
  {
    id: "conn-3",
    provider: "openai",
    name: "OpenAI Direct",
    email: "openai@example.com",
    authType: "apikey",
    apiKey: "sk-test",
  },
  {
    id: "conn-4",
    provider: "failing-provider",
    name: "Broken Provider",
    email: "fail@example.com",
    authType: "oauth",
    accessToken: "broken-token",
  },
];

vi.mock("@/lib/localDb", () => ({
  getProviderConnections: vi.fn(async () => mockConnections),
  getSettings: vi.fn(async () => ({ requireLogin: true, requireApiKey: true })),
  validateApiKey: vi.fn(async (key) => key === "valid-secret-key"),
  getApiKeyBySecret: vi.fn(async (key) =>
    key === "valid-secret-key" ? { key, isActive: true } : null,
  ),
}));

vi.mock("@/lib/auth/dashboardSession", () => ({
  getDashboardUser: vi.fn(async (req) => {
    const token = req?.cookies?.get?.("auth_token")?.value;
    return token === "valid-session"
      ? { id: "user-1", email: "admin@local" }
      : null;
  }),
  verifyDashboardAuthToken: vi.fn(async (token) => token === "valid-session"),
}));

vi.mock("@/lib/network/connectionProxy", () => ({
  resolveConnectionProxyConfig: vi.fn(async () => ({})),
}));

vi.mock("@/app/api/usage/[connectionId]/route.js", () => ({
  refreshAndUpdateCredentials: vi.fn(async (conn) => ({
    connection: conn,
    refreshed: false,
  })),
}));

vi.mock("open-sse/services/usage.js", () => ({
  getUsageForProvider: vi.fn(async (conn) => {
    if (conn.provider === "failing-provider") {
      throw new Error("Provider API timeout");
    }
    if (conn.provider === "claude") {
      return {
        quotas: {
          "session (5h)": {
            used: 20,
            total: 100,
            remainingPercentage: 80,
            resetAt: "2026-04-01T12:00:00Z",
          },
          "weekly (7d)": {
            used: 50,
            total: 100,
            remainingPercentage: 50,
            resetAt: "2026-04-05T12:00:00Z",
          },
        },
      };
    }
    if (conn.provider === "cursor") {
      return {
        quotas: {
          Plan: {
            used: 100,
            total: 100,
            remainingPercentage: 0,
            resetAt: "2026-04-02T00:00:00Z",
          },
        },
      };
    }
    return { message: "Usage API not implemented for openai" };
  }),
}));

import { GET, OPTIONS } from "@/app/api/v1/quota/route.js";

describe("/v1/quota endpoint", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("handles CORS OPTIONS preflight request", async () => {
    const res = await OPTIONS();
    expect(res.status).toBe(204);
    expect(res.headers.get("Access-Control-Allow-Origin")).toBe("*");
    expect(res.headers.get("Access-Control-Allow-Methods")).toContain("GET");
  });

  it("rejects unauthenticated GET requests with 401", async () => {
    const req = new Request("http://localhost:20128/v1/quota", {
      method: "GET",
      headers: {},
    });
    const res = await GET(req);
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body.success).toBe(false);
    expect(body.error).toBe("Unauthorized");
  });

  it("authenticates with Bearer API key and returns structured quotas", async () => {
    const req = new Request("http://localhost:20128/v1/quota", {
      method: "GET",
      headers: {
        authorization: "Bearer valid-secret-key",
      },
    });
    const res = await GET(req);
    expect(res.status).toBe(200);
    const data = await res.json();

    expect(data.success).toBe(true);
    expect(typeof data.timestamp).toBe("string");
    expect(data.summary).toBeDefined();
    expect(data.summary.totalConnections).toBe(4);
    expect(data.summary.activeConnections).toBe(1);
    expect(data.summary.depletedConnections).toBe(1);

    expect(data.quotas).toHaveLength(4);

    const claudeQuota = data.quotas.find((q) => q.provider === "claude");
    expect(claudeQuota).toBeDefined();
    expect(claudeQuota.status).toBe("active");
    expect(claudeQuota.remainingPercentage).toBe(50);
    expect(claudeQuota.resetAt).toBe("2026-04-01T12:00:00Z");
    expect(typeof claudeQuota.resetInSeconds).toBe("number");

    const cursorQuota = data.quotas.find((q) => q.provider === "cursor");
    expect(cursorQuota).toBeDefined();
    expect(cursorQuota.status).toBe("depleted");
    expect(cursorQuota.remainingPercentage).toBe(0);

    const openaiQuota = data.quotas.find((q) => q.provider === "openai");
    expect(openaiQuota).toBeDefined();
    expect(openaiQuota.status).toBe("unknown");

    const failingQuota = data.quotas.find(
      (q) => q.provider === "failing-provider",
    );
    expect(failingQuota).toBeDefined();
    expect(failingQuota.status).toBe("error");
    expect(failingQuota.details.error).toBe("Provider API timeout");
  });

  it("authenticates with x-api-key header", async () => {
    const req = new Request("http://localhost:20128/v1/quota", {
      method: "GET",
      headers: {
        "x-api-key": "valid-secret-key",
      },
    });
    const res = await GET(req);
    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
  });

  it("authenticates with session cookie", async () => {
    const req = new Request("http://localhost:20128/v1/quota", {
      method: "GET",
      headers: {
        cookie: "auth_token=valid-session",
      },
    });
    const res = await GET(req);
    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
  });
});
