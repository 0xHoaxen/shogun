import { redirect } from "next/navigation";

// Torii sends a successful login to /jobs; the root does the same.
export default function Home() {
  redirect("/jobs");
}
