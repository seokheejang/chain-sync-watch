import { DiffDetail } from "./diff-detail";

export default async function DiffDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <DiffDetail id={id} />;
}
