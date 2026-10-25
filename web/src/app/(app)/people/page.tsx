import { Suspense } from "react";

import { PeopleScreen } from "@/components/contacts/people-screen";

export default function PeoplePage() {
  // The screen reads the chosen person from the URL, which needs a boundary.
  return (
    <Suspense>
      <PeopleScreen />
    </Suspense>
  );
}
