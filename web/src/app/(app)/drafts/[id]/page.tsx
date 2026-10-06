import { DraftDetail } from "@/components/drafts/draft-detail";

interface DraftPageProps {
  params: Promise<{ id: string }>;
}

export default async function DraftPage({ params }: DraftPageProps) {
  const { id } = await params;
  return <DraftDetail id={id} />;
}
