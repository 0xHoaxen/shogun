import { LearningScreen } from "@/components/learning/learning-screen";
import { ProfileScreen } from "@/components/profile/profile-screen";

// The v2 Growth screen will replace both; until then the learning log and the
// profile suggestions share the page.
export default function GrowthPage() {
  return (
    <>
      <LearningScreen />
      <ProfileScreen />
    </>
  );
}
