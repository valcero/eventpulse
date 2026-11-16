import { NextRequest, NextResponse } from "next/server";

// Server-side proxy to the Go API. Reads env at runtime, keeps API_KEY off the browser.
export const dynamic = "force-dynamic";

async function proxy(req: NextRequest, { params }: { params: { path: string[] } }) {
  const base = process.env.BACKEND_URL ?? "http://localhost:8080";
  const target = `${base}/v1/${params.path.map(encodeURIComponent).join("/")}${req.nextUrl.search}`;

  const headers: Record<string, string> = {};
  const ct = req.headers.get("content-type");
  if (ct) headers["content-type"] = ct;
  if (process.env.API_KEY) headers["authorization"] = `Bearer ${process.env.API_KEY}`;

  const hasBody = !["GET", "HEAD"].includes(req.method);
  try {
    const res = await fetch(target, {
      method: req.method,
      headers,
      body: hasBody ? await req.text() : undefined,
      cache: "no-store",
    });
    return new NextResponse(res.status === 204 ? null : await res.text(), {
      status: res.status,
      headers: { "content-type": res.headers.get("content-type") ?? "application/json" },
    });
  } catch {
    return NextResponse.json({ error: "backend unavailable" }, { status: 502 });
  }
}

export { proxy as GET, proxy as POST, proxy as PATCH, proxy as DELETE };
