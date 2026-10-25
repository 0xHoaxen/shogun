import { redirect } from "next/navigation";

// Torii sends a successful login to /dashboard; the root does the same.
export default function Home() {
  redirect("/dashboard");
}
