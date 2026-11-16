"use client";

import { useState } from "react";
import { CheckCircle2, Clock, Globe, Percent, XCircle } from "lucide-react";
import { useStats } from "@/lib/hooks";
import { StatCard } from "@/components/StatCard";
import { RatioChart, ThroughputChart } from "@/components/Charts";
import { ErrorNote, PageHeader } from "@/components/ui";

const windows = [
  { label: "15m", minutes: 15 },
  { label: "1h", minutes: 60 },
  { label: "6h", minutes: 360 },
  { label: "24h", minutes: 1440 },
];

export default function OverviewPage() {
  const [minutes, setMinutes] = useState(60);
  const { data, error, isLoading } = useStats(minutes);

  const fmt = (n?: number) => (n === undefined ? "–" : n.toLocaleString());
  const done = (data?.totals.success ?? 0) + (data?.totals.failed ?? 0);

  return (
    <>
      <PageHeader title="Overview" subtitle="Delivery throughput and health. Refreshes every 5 seconds.">
        <div className="flex rounded-lg border border-slate-800 p-0.5">
          {windows.map((w) => (
            <button
              key={w.minutes}
              onClick={() => setMinutes(w.minutes)}
              className={`rounded-md px-3 py-1 text-sm transition ${
                minutes === w.minutes ? "bg-slate-800 text-white" : "text-slate-400 hover:text-slate-200"
              }`}
            >
              {w.label}
            </button>
          ))}
        </div>
      </PageHeader>

      <ErrorNote error={error} />

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
        <StatCard label="Successful" value={fmt(data?.totals.success)} icon={CheckCircle2} tone="text-emerald-400" />
        <StatCard label="Failed" value={fmt(data?.totals.failed)} icon={XCircle} tone="text-red-400" />
        <StatCard
          label="Success rate"
          value={data && done > 0 ? `${(data.success_rate * 100).toFixed(1)}%` : "–"}
          icon={Percent}
        />
        <StatCard label="Queue backlog" value={fmt(data?.backlog)} icon={Clock} tone="text-amber-400" />
        <StatCard label="Active endpoints" value={fmt(data?.active_endpoints)} icon={Globe} />
      </div>

      <div className="mt-6 grid gap-4 lg:grid-cols-3">
        <div className="card p-5 lg:col-span-2">
          <h2 className="mb-4 text-sm font-medium text-slate-300">Deliveries per minute</h2>
          {isLoading || !data ? <div className="h-[280px] animate-pulse rounded-lg bg-slate-800/40" /> : <ThroughputChart series={data.series} />}
        </div>
        <div className="card p-5">
          <h2 className="mb-4 text-sm font-medium text-slate-300">Success vs. failure</h2>
          <RatioChart success={data?.totals.success ?? 0} failed={data?.totals.failed ?? 0} />
          <div className="mt-2 flex justify-center gap-6 text-xs text-slate-400">
            <span className="flex items-center gap-1.5"><i className="h-2 w-2 rounded-full bg-emerald-500" />Success</span>
            <span className="flex items-center gap-1.5"><i className="h-2 w-2 rounded-full bg-red-500" />Failed</span>
          </div>
        </div>
      </div>
    </>
  );
}
