"use client";

import { useState, useEffect, useMemo } from "react";
import PropTypes from "prop-types";
import Card from "@/shared/components/Card";

const fmt = (n) => new Intl.NumberFormat().format(n || 0);
const fmtCost = (n) => `$${(n || 0).toFixed(2)}`;
const fmtIdr = (n) =>
  `~Rp${new Intl.NumberFormat("id-ID").format(Math.round(n || 0))}`;

export default function OverviewCards({ stats }) {
  const [exchangeRate, setExchangeRate] = useState(16200);

  useEffect(() => {
    const CACHE_KEY_RATE = "app_usd_idr_rate";
    const CACHE_KEY_TIME = "app_usd_idr_time";
    const LEGACY_CACHE_KEY = "rate_usd_idr";
    const CACHE_TTL = 5 * 60 * 1000;
    const FALLBACK_RATE = 16200;

    let isMounted = true;

    try {
      const storedRate = parseFloat(localStorage.getItem(CACHE_KEY_RATE));
      const lastSync = parseInt(localStorage.getItem(CACHE_KEY_TIME), 10) || 0;
      if (storedRate && !isNaN(storedRate) && storedRate > 0) {
        setExchangeRate(storedRate);
        if (Date.now() - lastSync < CACHE_TTL) {
          return;
        }
      } else {
        const rawLegacy = localStorage.getItem(LEGACY_CACHE_KEY);
        if (rawLegacy) {
          const parsed = JSON.parse(rawLegacy);
          if (parsed && typeof parsed.rate === "number") {
            setExchangeRate(parsed.rate);
            if (Date.now() - (parsed.timestamp || 0) < CACHE_TTL) {
              return;
            }
          }
        }
      }
    } catch {}

    fetch("https://open.er-api.com/v6/latest/USD")
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!isMounted) return;
        const rate = data?.rates?.IDR;
        if (typeof rate === "number" && rate > 0) {
          setExchangeRate(rate);
          try {
            localStorage.setItem(CACHE_KEY_RATE, rate.toString());
            localStorage.setItem(CACHE_KEY_TIME, Date.now().toString());
            localStorage.setItem(
              LEGACY_CACHE_KEY,
              JSON.stringify({ rate, timestamp: Date.now() }),
            );
          } catch {}
        }
      })
      .catch(() => {});

    return () => {
      isMounted = false;
    };
  }, []);

  const topModelText = useMemo(() => {
    if (!stats?.byModel || typeof stats.byModel !== "object") return null;
    let top = null;
    for (const [key, val] of Object.entries(stats.byModel)) {
      const count = val?.requests || 0;
      if (count > 0 && (!top || count > top.requests)) {
        const name = val?.rawModel || key.split(" ")[0] || key;
        top = { name, requests: count };
      }
    }
    return top ? `${top.name} (${fmt(top.requests)})` : null;
  }, [stats?.byModel]);

  const totalReqs = stats?.totalRequests || 0;
  const totalPrompt = stats?.totalPromptTokens || 0;
  const totalCached = stats?.totalCachedTokens || 0;
  const totalCompletion = stats?.totalCompletionTokens || 0;
  const totalCost = stats?.totalCost || 0;

  const reqPerHour = (totalReqs / 24).toFixed(1);
  const card0Subtitle = topModelText || `~${reqPerHour} req/jam`;
  const promptPerReq = totalReqs > 0 ? Math.round(totalPrompt / totalReqs) : 0;
  const hitRate =
    totalPrompt > 0 ? ((totalCached / totalPrompt) * 100).toFixed(2) : "0.00";
  const ratio =
    totalCompletion > 0 ? (totalPrompt / totalCompletion).toFixed(1) : "0.0";
  const idrCost = totalCost * exchangeRate;

  return (
    <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-5 sm:gap-4">
      <Card className="flex min-w-0 flex-col items-center text-center gap-1 px-3 py-3 sm:px-4">
        <span className="text-text-muted text-xs uppercase font-semibold sm:text-sm">
          Total Requests
        </span>
        <span
          className="w-full truncate text-lg font-bold xl:text-xl"
          title={fmt(totalReqs)}
        >
          {fmt(totalReqs)}
        </span>
        <span
          className="w-full truncate text-xs text-text-muted"
          title={card0Subtitle}
        >
          {card0Subtitle}
        </span>
      </Card>
      <Card className="flex min-w-0 flex-col items-center text-center gap-1 px-3 py-3 sm:px-4">
        <span className="text-text-muted text-xs uppercase font-semibold sm:text-sm">
          Total Input Tokens
        </span>
        <span
          className="w-full truncate text-lg font-bold text-primary xl:text-xl"
          title={fmt(totalPrompt)}
        >
          {fmt(totalPrompt)}
        </span>
        <span className="w-full truncate text-xs text-text-muted">
          ~{fmt(promptPerReq)} tok/req
        </span>
      </Card>
      <Card className="flex min-w-0 flex-col items-center text-center gap-1 px-3 py-3 sm:px-4">
        <span className="text-text-muted text-xs uppercase font-semibold sm:text-sm">
          Cached Tokens
        </span>
        <span
          className="w-full truncate text-lg font-bold text-info xl:text-xl"
          title={fmt(totalCached)}
        >
          {fmt(totalCached)}
        </span>
        <span className="w-full truncate text-xs text-text-muted">
          {hitRate}% hit rate
        </span>
      </Card>
      <Card className="flex min-w-0 flex-col items-center text-center gap-1 px-3 py-3 sm:px-4">
        <span className="text-text-muted text-xs uppercase font-semibold sm:text-sm">
          Output Tokens
        </span>
        <span
          className="w-full truncate text-lg font-bold text-success xl:text-xl"
          title={fmt(totalCompletion)}
        >
          {fmt(totalCompletion)}
        </span>
        <span className="w-full truncate text-xs text-text-muted">
          1 : {ratio} ratio
        </span>
      </Card>
      <Card className="flex min-w-0 flex-col items-center text-center gap-1 px-3 py-3 sm:px-4">
        <span className="text-text-muted text-xs uppercase font-semibold sm:text-sm">
          Est. Cost
        </span>
        <span
          className="w-full truncate text-lg font-bold text-warning xl:text-xl"
          title={`~${fmtCost(totalCost)}`}
        >
          ~{fmtCost(totalCost)}
        </span>
        <span
          className="w-full truncate text-xs text-text-muted"
          title={fmtIdr(idrCost)}
        >
          {fmtIdr(idrCost)}
        </span>
      </Card>
    </div>
  );
}

OverviewCards.propTypes = {
  stats: PropTypes.object.isRequired,
};
