"use client";

import type { Application } from "@/lib/api";

export function AppSelect({
  apps,
  value,
  onChange,
  allowAll,
}: {
  apps: Application[] | undefined;
  value: string;
  onChange: (id: string) => void;
  allowAll?: boolean;
}) {
  return (
    <select className="input !w-56" value={value} onChange={(e) => onChange(e.target.value)} aria-label="Application">
      {allowAll && <option value="">All applications</option>}
      {!allowAll && !apps?.length && <option value="">No applications</option>}
      {apps?.map((a) => (
        <option key={a.id} value={a.id}>
          {a.name}
        </option>
      ))}
    </select>
  );
}
