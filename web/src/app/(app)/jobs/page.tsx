import { PageHeader } from "@/components/shell/page-header";

// The board arrives in P5.2a.
export default function JobsPage() {
  return (
    <PageHeader
      title="Your pipeline."
      description="Every role by stage. Cards drag between stages and each move is logged."
    />
  );
}
