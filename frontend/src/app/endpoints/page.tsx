"use client";

import { useEffect, useState } from "react";
import { Check, Copy, Eye, EyeOff, Plus, RefreshCw, Send, Trash2 } from "lucide-react";
import { mutate } from "swr";
import { api, type Endpoint } from "@/lib/api";
import { useApplications, useEndpoints } from "@/lib/hooks";
import { AppSelect } from "@/components/AppSelect";
import { EmptyState, ErrorNote, Modal, PageHeader, timeAgo } from "@/components/ui";

export default function EndpointsPage() {
  const apps = useApplications();
  const [appId, setAppId] = useState("");
  const endpoints = useEndpoints(appId || undefined);
  const [modal, setModal] = useState<"app" | "endpoint" | null>(null);
  const [error, setError] = useState<Error>();
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (!appId && apps.data?.length) setAppId(apps.data[0].id);
  }, [apps.data, appId]);

  const refresh = () => mutate(`/endpoints?app_id=${appId}`);
  const run = async (fn: () => Promise<unknown>) => {
    setError(undefined);
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(e as Error);
    }
  };

  const sendTest = () =>
    run(async () => {
      const r = await api.sendEvent(appId, "test.ping", { message: "Hello from EventPulse", sent_at: new Date().toISOString() });
      setNotice(`Test event queued for ${r.deliveries} endpoint${r.deliveries === 1 ? "" : "s"}.`);
      setTimeout(() => setNotice(""), 4000);
    });

  return (
    <>
      <PageHeader title="Endpoints" subtitle="Webhook destinations and their signing secrets.">
        <AppSelect apps={apps.data} value={appId} onChange={setAppId} />
        <button className="btn-ghost" onClick={() => setModal("app")}>
          <Plus className="h-4 w-4" /> App
        </button>
        <button className="btn-ghost" disabled={!appId} onClick={sendTest}>
          <Send className="h-4 w-4" /> Test event
        </button>
        <button className="btn-primary" disabled={!appId} onClick={() => setModal("endpoint")}>
          <Plus className="h-4 w-4" /> Add endpoint
        </button>
      </PageHeader>

      <ErrorNote error={error ?? endpoints.error ?? apps.error} />
      {notice && (
        <div className="mb-4 rounded-lg border border-emerald-900 bg-emerald-950/40 px-4 py-2 text-sm text-emerald-300">{notice}</div>
      )}

      <div className="card overflow-hidden">
        <table className="w-full">
          <thead className="border-b border-slate-800">
            <tr>
              <th className="th">URL</th>
              <th className="th">Signing secret</th>
              <th className="th">Status</th>
              <th className="th">Created</th>
              <th className="th text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {endpoints.data?.map((ep) => (
              <EndpointRow key={ep.id} ep={ep} run={run} />
            ))}
          </tbody>
        </table>
        {!apps.data?.length && <EmptyState message="Create an application to get started." />}
        {!!apps.data?.length && endpoints.data?.length === 0 && <EmptyState message="No endpoints yet. Add one to start receiving events." />}
      </div>

      {modal === "app" && (
        <FormModal
          title="New application"
          label="Name"
          placeholder="My SaaS"
          onClose={() => setModal(null)}
          onSubmit={async (name) => {
            const app = await api.createApplication(name);
            await mutate("/applications?limit=200");
            setAppId(app.id);
          }}
        />
      )}
      {modal === "endpoint" && (
        <FormModal
          title="Add endpoint"
          label="Destination URL"
          placeholder="https://example.com/webhooks"
          type="url"
          onClose={() => setModal(null)}
          onSubmit={async (url) => {
            await api.createEndpoint(appId, url);
            await refresh();
          }}
        />
      )}
    </>
  );
}

function EndpointRow({ ep, run }: { ep: Endpoint; run: (fn: () => Promise<unknown>) => Promise<void> }) {
  const [show, setShow] = useState(false);
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    await navigator.clipboard.writeText(ep.secret);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <tr className="hover:bg-slate-900/60">
      <td className="td max-w-xs truncate font-mono text-xs text-slate-300" title={ep.url}>{ep.url}</td>
      <td className="td">
        <div className="flex items-center gap-2 font-mono text-xs text-slate-400">
          <span>{show ? ep.secret : "whsec_" + "•".repeat(16)}</span>
          <button onClick={() => setShow(!show)} className="text-slate-500 hover:text-slate-300" aria-label="Toggle secret">
            {show ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
          </button>
          <button onClick={copy} className="text-slate-500 hover:text-slate-300" aria-label="Copy secret">
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
          </button>
        </div>
      </td>
      <td className="td">
        <button
          onClick={() => run(() => api.updateEndpoint(ep.id, { is_active: !ep.is_active }))}
          className={`rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset transition ${
            ep.is_active
              ? "bg-emerald-500/10 text-emerald-400 ring-emerald-500/30"
              : "bg-slate-700/30 text-slate-400 ring-slate-600"
          }`}
        >
          {ep.is_active ? "Active" : "Disabled"}
        </button>
      </td>
      <td className="td text-slate-400">{timeAgo(ep.created_at)}</td>
      <td className="td">
        <div className="flex justify-end gap-2">
          <button
            className="btn-ghost !px-2"
            title="Rotate secret"
            onClick={() => confirm("Rotate the signing secret? Receivers must be updated.") && run(() => api.rotateSecret(ep.id))}
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
          <button
            className="btn-danger !px-2"
            title="Delete endpoint"
            onClick={() => confirm("Delete this endpoint and its delivery history?") && run(() => api.deleteEndpoint(ep.id))}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </td>
    </tr>
  );
}

function FormModal({
  title,
  label,
  placeholder,
  type = "text",
  onClose,
  onSubmit,
}: {
  title: string;
  label: string;
  placeholder: string;
  type?: string;
  onClose: () => void;
  onSubmit: (value: string) => Promise<void>;
}) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error>();

  return (
    <Modal title={title} onClose={onClose}>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(undefined);
          try {
            await onSubmit(value.trim());
            onClose();
          } catch (err) {
            setError(err as Error);
            setBusy(false);
          }
        }}
      >
        <ErrorNote error={error} />
        <label className="mb-1 block text-sm text-slate-400">{label}</label>
        <input className="input" type={type} placeholder={placeholder} value={value} onChange={(e) => setValue(e.target.value)} required autoFocus />
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary" disabled={busy || !value.trim()}>{busy ? "Saving…" : "Create"}</button>
        </div>
      </form>
    </Modal>
  );
}
