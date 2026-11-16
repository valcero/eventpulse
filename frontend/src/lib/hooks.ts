"use client";

import useSWR from "swr";
import { fetcher, type Application, type Delivery, type Endpoint, type Stats } from "./api";

const POLL_MS = 5000;

export function useStats(windowMinutes: number) {
  return useSWR<Stats>(`/stats?window=${windowMinutes}`, fetcher, {
    refreshInterval: POLL_MS,
    keepPreviousData: true,
  });
}

export function useApplications() {
  return useSWR<Application[]>("/applications?limit=200", fetcher, { refreshInterval: 30000 });
}

export function useEndpoints(appId?: string) {
  const qs = appId ? `?app_id=${appId}` : "";
  return useSWR<Endpoint[]>(appId ? `/endpoints${qs}` : null, fetcher, { refreshInterval: POLL_MS });
}

export function useDeliveries(opts: { appId?: string; status?: string; limit: number; offset: number }) {
  const p = new URLSearchParams({ limit: String(opts.limit), offset: String(opts.offset) });
  if (opts.appId) p.set("app_id", opts.appId);
  if (opts.status) p.set("status", opts.status);
  return useSWR<Delivery[]>(`/deliveries?${p}`, fetcher, {
    refreshInterval: POLL_MS,
    keepPreviousData: true,
  });
}
