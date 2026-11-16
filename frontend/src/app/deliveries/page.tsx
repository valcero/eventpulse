"use client";

import { Fragment, useState } from "react";
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, RotateCw } from "lucide-react";
import { useSWRConfig } from "swr";
import { api, type Delivery } from "@/lib/api";
import { useApplications, useDeliveries } from "@/lib/hooks";
import { AppSelect } from "@/components/AppSelect";
import { EmptyState, ErrorNote, PageHeader, StatusBadge, timeAgo } from "@/components/ui";

const PAGE = 25;
const statuses = ["", "pending", "success", "failed"] as const;

export default function DeliveriesPage() {
  const apps = useApplications();
  const [appId, setAppId] = useState("");
  const [status, setStatus] = useState<(typeof statuses)[number]>("");
  const [page, setPage] = useState(0);
  const [open, setOpen] = useState<string | null>(null);
  const [error, setError] = useState<Error>();
  const { mutate } = useSWRConfig();

  const { data, error: loadError, isLoading } = useDeliveries({ appId, status, limit: PAGE, offset: page * PAGE });

  const redeliver = async (d: Delivery) => {
    setError(undefined);
    try {
      await api.redeliver(d.id);
      await mutate(() => true); // revalidate all delivery/stat queries
    } catch (e) {
      setError(e as Error);
    }
  };

  return (
    <>
      <PageHeader title="Deliveries" subtitle="Every delivery attempt, with payloads and response codes.">
        <AppSelect apps={apps.data} value={appId} allowAll onChange={(v) => { setAppId(v); setPage(0); }} />
        <select
          className="input !w-36"
          value={status}
          onChange={(e) => { setStatus(e.target.value as typeof status); setPage(0); }}
          aria-label="Status filter"
        >
          {statuses.map((s) => (
            <option key={s} value={s}>{s || "All statuses"}</option>
          ))}
        </select>
      </PageHeader>

      <ErrorNote error={error ?? loadError} />

      <div className="card overflow-hidden">
        <table className="w-full">
          <thead className="border-b border-slate-800">
            <tr>
              <th className="th w-8" />
              <th className="th">Event</th>
              <th className="th">Endpoint</th>
              <th className="th">Status</th>
              <th className="th">HTTP</th>
              <th className="th">Attempts</th>
              <th className="th">Created</th>
              <th className="th text-right">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {data?.map((d) => (
              <Fragment key={d.id}>
                <tr className="hover:bg-slate-900/60">
                  <td className="td">
                    <button onClick={() => setOpen(open === d.id ? null : d.id)} className="text-slate-500 hover:text-slate-300" aria-label="Inspect payload">
                      {open === d.id ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                    </button>
                  </td>
                  <td className="td font-mono text-xs text-slate-300">{d.event_type}</td>
                  <td className="td max-w-[220px] truncate font-mono text-xs text-slate-400" title={d.url}>{d.url}</td>
                  <td className="td"><StatusBadge status={d.status} /></td>
                  <td className="td font-mono text-xs">{d.status_code ?? "–"}</td>
                  <td className="td text-slate-400">{d.attempts}</td>
                  <td className="td text-slate-400">{timeAgo(d.created_at)}</td>
                  <td className="td text-right">
                    <button className="btn-ghost" onClick={() => redeliver(d)}>
                      <RotateCw className="h-3.5 w-3.5" /> Redeliver
                    </button>
                  </td>
                </tr>
                {open === d.id && (
                  <tr className="bg-slate-950/60">
                    <td colSpan={8} className="px-6 py-4">
                      <dl className="mb-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs text-slate-400 md:grid-cols-3">
                        <div><dt className="inline text-slate-500">Delivery: </dt><dd className="inline font-mono">{d.id}</dd></div>
                        <div><dt className="inline text-slate-500">Event: </dt><dd className="inline font-mono">{d.event_id}</dd></div>
                        {d.status === "pending" && (
                          <div><dt className="inline text-slate-500">Next attempt: </dt><dd className="inline">{new Date(d.next_retry_at).toLocaleString()}</dd></div>
                        )}
                      </dl>
                      <pre className="max-h-64 overflow-auto rounded-lg border border-slate-800 bg-slate-950 p-3 font-mono text-xs text-slate-300">
                        {JSON.stringify(d.payload, null, 2)}
                      </pre>
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
        {!isLoading && data?.length === 0 && <EmptyState message="No deliveries match these filters." />}
      </div>

      <div className="mt-4 flex items-center justify-end gap-2 text-sm text-slate-400">
        <span>Page {page + 1}</span>
        <button className="btn-ghost !px-2" disabled={page === 0} onClick={() => setPage(page - 1)} aria-label="Previous page">
          <ChevronLeft className="h-4 w-4" />
        </button>
        <button className="btn-ghost !px-2" disabled={(data?.length ?? 0) < PAGE} onClick={() => setPage(page + 1)} aria-label="Next page">
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </>
  );
}
