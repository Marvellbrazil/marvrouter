import "open-sse/index.js";
import { NextResponse } from "next/server";
import {
  getProviderConnections,
  getSettings,
  validateApiKey,
  getApiKeyBySecret,
} from "@/lib/localDb";
import {
  getDashboardUser,
  verifyDashboardAuthToken,
} from "@/lib/auth/dashboardSession";
import { getUsageForProvider } from "open-sse/services/usage.js";
import { resolveConnectionProxyConfig } from "@/lib/network/connectionProxy";
import {
  USAGE_APIKEY_PROVIDERS,
  USAGE_SUPPORTED_PROVIDERS,
} from "@/shared/constants/providers";
import { refreshAndUpdateCredentials } from "@/app/api/usage/[connectionId]/route.js";

const CORS_HEADERS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, OPTIONS",
  "Access-Control-Allow-Headers": "Authorization, Content-Type, x-api-key",
};

export async function OPTIONS() {
  return new NextResponse(null, {
    status: 204,
    headers: CORS_HEADERS,
  });
}

async function isAuthorized(request) {
  const authHeader =
    request.headers.get("authorization") ||
    request.headers.get("Authorization");
  let apiKey = null;
  if (authHeader && authHeader.toLowerCase().startsWith("bearer ")) {
    apiKey = authHeader.slice(7).trim();
  }
  if (!apiKey) {
    apiKey = request.headers.get("x-api-key")?.trim() || null;
  }

  if (apiKey) {
    const isValid = await validateApiKey(apiKey);
    if (isValid) return true;
    const keyRecord = await getApiKeyBySecret(apiKey);
    if (keyRecord?.isActive) return true;
  }

  const user = await getDashboardUser(request);
  if (user) return true;

  const cookieToken = request.cookies?.get?.("auth_token")?.value;
  if (cookieToken) {
    const isTokenValid = await verifyDashboardAuthToken(cookieToken);
    if (isTokenValid) return true;
  }

  const rawCookie = request.headers.get("cookie");
  if (rawCookie) {
    const match = rawCookie.match(/(?:^|;\s*)auth_token=([^;]+)/);
    if (match) {
      const isTokenValid = await verifyDashboardAuthToken(
        decodeURIComponent(match[1]),
      );
      if (isTokenValid) return true;
    }
  }

  const settings = await getSettings().catch(() => null);
  if (settings && settings.requireLogin === false && !settings.requireApiKey) {
    return true;
  }

  return false;
}

function extractConnectionQuota(conn, usage) {
  if (!usage || typeof usage !== "object") {
    return {
      status: "unknown",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: {},
    };
  }

  if (usage.error) {
    return {
      status: "error",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: usage,
    };
  }

  if (usage.message && !usage.quotas) {
    const lower = String(usage.message).toLowerCase();
    const isErr =
      lower.includes("error") ||
      lower.includes("unauthorized") ||
      lower.includes("failed") ||
      lower.includes("expired");
    return {
      status: isErr ? "error" : "unknown",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: usage,
    };
  }

  const quotasObj = usage.quotas;
  if (
    !quotasObj ||
    typeof quotasObj !== "object" ||
    Object.keys(quotasObj).length === 0
  ) {
    return {
      status: "unknown",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: usage,
    };
  }

  const percentages = [];
  let earliestResetMs = Number.POSITIVE_INFINITY;
  let earliestResetAt = null;

  for (const q of Object.values(quotasObj)) {
    if (!q || typeof q !== "object") continue;

    if (
      typeof q.remainingPercentage === "number" &&
      Number.isFinite(q.remainingPercentage)
    ) {
      percentages.push(
        Math.max(0, Math.min(100, Math.round(q.remainingPercentage))),
      );
    } else if (
      typeof q.remaining === "number" &&
      Number.isFinite(q.remaining) &&
      (q.total === 100 || q.total === undefined)
    ) {
      percentages.push(Math.max(0, Math.min(100, Math.round(q.remaining))));
    } else if (
      typeof q.total === "number" &&
      q.total > 0 &&
      typeof q.used === "number" &&
      Number.isFinite(q.used)
    ) {
      const pct = Math.max(
        0,
        Math.min(100, Math.round(((q.total - q.used) / q.total) * 100)),
      );
      percentages.push(pct);
    }

    if (q.resetAt) {
      const ms = new Date(q.resetAt).getTime();
      if (Number.isFinite(ms) && ms < earliestResetMs) {
        earliestResetMs = ms;
        earliestResetAt = q.resetAt;
      }
    }
  }

  const remainingPercentage =
    percentages.length > 0 ? Math.min(...percentages) : 0;
  let status = "unknown";
  if (percentages.length > 0) {
    status = remainingPercentage <= 0 ? "depleted" : "active";
  }

  let resetInSeconds = null;
  if (earliestResetAt && Number.isFinite(earliestResetMs)) {
    resetInSeconds = Math.max(
      0,
      Math.round((earliestResetMs - Date.now()) / 1000),
    );
  }

  return {
    status,
    remainingPercentage,
    resetAt: earliestResetAt,
    resetInSeconds,
    details: usage,
  };
}

async function resolveConnectionQuota(conn) {
  const connectionId = conn.id;
  const provider = conn.provider;
  const name =
    conn.name ||
    conn.displayName ||
    conn.providerSpecificData?.githubLogin ||
    conn.providerSpecificData?.githubName ||
    provider;
  const email =
    conn.email ||
    conn.providerSpecificData?.email ||
    conn.providerSpecificData?.githubEmail ||
    null;
  const authType = conn.authType || (conn.apiKey ? "apikey" : "oauth");

  const isOAuth = conn.authType === "oauth";
  const isApikeyAuth =
    conn.authType === "apikey" || conn.authType === "api_key";
  const isApikeyEligible =
    isApikeyAuth &&
    (USAGE_APIKEY_PROVIDERS.includes(provider) ||
      USAGE_SUPPORTED_PROVIDERS.includes(provider));
  const isEligible =
    isOAuth || isApikeyEligible || USAGE_SUPPORTED_PROVIDERS.includes(provider);

  if (!isEligible) {
    return {
      connectionId,
      provider,
      name,
      email,
      authType,
      status: "unknown",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: { message: "Usage tracking not supported for this provider" },
    };
  }

  try {
    const proxyConfig = await resolveConnectionProxyConfig(
      conn.providerSpecificData,
    ).catch(() => ({}));
    const proxyOptions = {
      connectionProxyEnabled: proxyConfig.connectionProxyEnabled === true,
      connectionProxyUrl: proxyConfig.connectionProxyUrl || "",
      connectionNoProxy: proxyConfig.connectionNoProxy || "",
      vercelRelayUrl: proxyConfig.vercelRelayUrl || "",
      strictProxy: false,
    };

    let activeConn = conn;
    if (isOAuth) {
      try {
        const refreshed = await refreshAndUpdateCredentials(
          conn,
          false,
          proxyOptions,
        );
        if (refreshed?.connection) {
          activeConn = refreshed.connection;
        }
      } catch (refreshErr) {
        return {
          connectionId,
          provider,
          name,
          email,
          authType,
          status: "error",
          remainingPercentage: 0,
          resetAt: null,
          resetInSeconds: null,
          details: { error: refreshErr.message },
        };
      }
    }

    const usage = await getUsageForProvider(activeConn, proxyOptions);
    const parsed = extractConnectionQuota(activeConn, usage);

    return {
      connectionId,
      provider,
      name,
      email,
      authType,
      status: parsed.status,
      remainingPercentage: parsed.remainingPercentage,
      resetAt: parsed.resetAt,
      resetInSeconds: parsed.resetInSeconds,
      details: parsed.details,
    };
  } catch (err) {
    return {
      connectionId,
      provider,
      name,
      email,
      authType,
      status: "error",
      remainingPercentage: 0,
      resetAt: null,
      resetInSeconds: null,
      details: { error: err.message },
    };
  }
}

export async function GET(request) {
  const authorized = await isAuthorized(request);
  if (!authorized) {
    return NextResponse.json(
      { success: false, error: "Unauthorized" },
      { status: 401, headers: CORS_HEADERS },
    );
  }

  try {
    const connections = await getProviderConnections();
    const quotas = await Promise.all(
      (connections || []).map((conn) => resolveConnectionQuota(conn)),
    );

    const totalConnections = quotas.length;
    const activeConnections = quotas.filter(
      (q) => q.status === "active",
    ).length;
    const depletedConnections = quotas.filter(
      (q) => q.status === "depleted",
    ).length;

    return NextResponse.json(
      {
        success: true,
        timestamp: new Date().toISOString(),
        summary: {
          totalConnections,
          activeConnections,
          depletedConnections,
        },
        quotas,
      },
      { headers: CORS_HEADERS },
    );
  } catch (err) {
    return NextResponse.json(
      { success: false, error: err.message },
      { status: 500, headers: CORS_HEADERS },
    );
  }
}
