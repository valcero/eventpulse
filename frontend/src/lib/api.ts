export type DeliveryStatus = "pending" | "success" | "failed";

export interface Application {
  id: string;
  name: string;
  owner_id: string;
  created_at: string;
}

export interface Endpoint {
  id: string;
  app_id: string;
  url: string;
  secret: string;
  is_active: boolean;
  created_at: string;
}

export interface Delivery {
  id: string;
  event_id: string;
  endpoint_id: string;
  status: DeliveryStatus;
  status_code: number | null;
  attempts: number;
  next_retry_at: string;
  created_at: string;
  event_type: string;
  payload: unknown;
  url: string;
}

export interface SeriesPoint {
  time: string;
  success: number;
  failed: number;
  pending: number;
}

export interface Stats {
  window_minutes: number;
  active_endpoints: number;
  backlog: number;
  totals: { success: number; failed: number; pending: number };
  success_rate: number;
  series: SeriesPoint[];
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

// All calls go through the Next.js proxy route (/api/*) so the API key and
// backend URL stay on the server.
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
    cache: "no-store",
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = (await res.json()).error ?? msg;
    } catch {}
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const fetcher = <T,>(path: string) => request<T>(path);

const json = (method: string, body?: unknown): RequestInit => ({
  method,
  body: body === undefined ? undefined : JSON.stringify(body),
});

export const api = {
  createApplication: (name: string) => request<Application>("/applications", json("POST", { name })),
  createEndpoint: (appId: string, url: string) =>
    request<Endpoint>(`/applications/${appId}/endpoints`, json("POST", { url })),
  updateEndpoint: (id: string, patch: { url?: string; is_active?: boolean }) =>
    request<Endpoint>(`/endpoints/${id}`, json("PATCH", patch)),
  rotateSecret: (id: string) => request<Endpoint>(`/endpoints/${id}/rotate-secret`, json("POST")),
  deleteEndpoint: (id: string) => request<void>(`/endpoints/${id}`, json("DELETE")),
  sendEvent: (appId: string, event_type: string, payload: unknown) =>
    request<{ event_id: string; deliveries: number }>(
      `/applications/${appId}/events`,
      json("POST", { event_type, payload }),
    ),
  redeliver: (id: string) => request<{ status: string }>(`/deliveries/${id}/redeliver`, json("POST")),
};
