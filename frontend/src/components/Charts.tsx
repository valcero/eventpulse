"use client";

import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { SeriesPoint } from "@/lib/api";

const tooltipStyle = {
  background: "#0f172a",
  border: "1px solid #1e293b",
  borderRadius: 8,
  fontSize: 12,
};

export function ThroughputChart({ series }: { series: SeriesPoint[] }) {
  const data = series.map((p) => ({
    ...p,
    label: new Date(p.time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
  }));
  return (
    <ResponsiveContainer width="100%" height={280}>
      <AreaChart data={data} margin={{ left: -20, right: 8, top: 8 }}>
        <defs>
          <linearGradient id="gSuccess" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity={0.5} />
            <stop offset="100%" stopColor="#10b981" stopOpacity={0} />
          </linearGradient>
          <linearGradient id="gFailed" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#ef4444" stopOpacity={0.5} />
            <stop offset="100%" stopColor="#ef4444" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid stroke="#1e293b" vertical={false} />
        <XAxis dataKey="label" stroke="#64748b" fontSize={11} tickLine={false} minTickGap={32} />
        <YAxis stroke="#64748b" fontSize={11} tickLine={false} allowDecimals={false} />
        <Tooltip contentStyle={tooltipStyle} />
        <Area type="monotone" dataKey="success" stackId="1" stroke="#10b981" fill="url(#gSuccess)" name="Success" />
        <Area type="monotone" dataKey="pending" stackId="1" stroke="#f59e0b" fill="#f59e0b22" name="Pending" />
        <Area type="monotone" dataKey="failed" stackId="1" stroke="#ef4444" fill="url(#gFailed)" name="Failed" />
      </AreaChart>
    </ResponsiveContainer>
  );
}

export function RatioChart({ success, failed }: { success: number; failed: number }) {
  const empty = success + failed === 0;
  const data = empty
    ? [{ name: "No data", value: 1, color: "#1e293b" }]
    : [
        { name: "Success", value: success, color: "#10b981" },
        { name: "Failed", value: failed, color: "#ef4444" },
      ];
  return (
    <ResponsiveContainer width="100%" height={280}>
      <PieChart>
        <Pie data={data} dataKey="value" innerRadius={70} outerRadius={100} paddingAngle={empty ? 0 : 3} stroke="none">
          {data.map((d) => (
            <Cell key={d.name} fill={d.color} />
          ))}
        </Pie>
        {!empty && <Tooltip contentStyle={tooltipStyle} />}
      </PieChart>
    </ResponsiveContainer>
  );
}
