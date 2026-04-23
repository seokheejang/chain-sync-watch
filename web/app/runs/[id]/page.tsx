import { RunDetail } from "./run-detail";

// Next.js 16 App Router delivers dynamic route params as a Promise;
// server components await them and pass the resolved primitive to
// a "use client" child. Keeping this shell a server component
// avoids a needless client-side navigation hop for the id.
export default async function RunDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <RunDetail id={id} />;
}
