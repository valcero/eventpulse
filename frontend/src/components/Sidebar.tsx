"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, Globe, ScrollText, Zap } from "lucide-react";

const links = [
  { href: "/", label: "Overview", icon: Activity },
  { href: "/endpoints", label: "Endpoints", icon: Globe },
  { href: "/deliveries", label: "Deliveries", icon: ScrollText },
];

export function Sidebar() {
  const path = usePathname();
  return (
    <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-slate-800 bg-slate-950 p-4 md:flex">
      <div className="mb-8 flex items-center gap-2 px-2">
        <span className="grid h-8 w-8 place-items-center rounded-lg bg-brand-500">
          <Zap className="h-4 w-4 text-white" />
        </span>
        <span className="text-lg font-semibold tracking-tight text-white">EventPulse</span>
      </div>
      <nav className="space-y-1">
        {links.map(({ href, label, icon: Icon }) => {
          const active = href === "/" ? path === "/" : path.startsWith(href);
          return (
            <Link
              key={href}
              href={href}
              className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition ${
                active ? "bg-slate-800 text-white" : "text-slate-400 hover:bg-slate-900 hover:text-slate-200"
              }`}
            >
              <Icon className="h-4 w-4" />
              {label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
