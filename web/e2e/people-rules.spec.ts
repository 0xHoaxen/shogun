import { expect, test } from "@playwright/test";

import { daysSince, eventTimeText, isFollowUpDue, lastContactText } from "../src/components/contacts/follow-up";
import { countByRelationship, historyText, initialsOf, relationshipOf } from "../src/components/contacts/relationship";
import { channelFor } from "../src/components/contacts/use-draft-a-note";
import { DraftChannel } from "../src/gen/shogun/api/v1/drafts_pb";

const TODAY = "2026-10-25";

test.describe("isFollowUpDue", () => {
  for (const [next, want] of [
    ["2026-10-25", true],
    ["2026-10-24", true],
    ["2025-01-01", true],
    ["2026-10-26", false],
    ["", false],
  ] as const) {
    test(`${next || "no date"}`, () => expect(isFollowUpDue(next, TODAY)).toBe(want));
  }
});

test.describe("lastContactText", () => {
  for (const [last, want] of [
    ["", "never"],
    ["2026-10-25", "today"],
    ["2026-10-26", "today"],
    ["2026-10-24", "1d ago"],
    ["2026-10-19", "6d ago"],
    ["2026-10-18", "1w ago"],
    ["2026-10-04", "3w ago"],
    ["2026-09-25", "1mo ago"],
    ["2026-01-01", "9mo ago"],
    ["2025-10-25", "1y ago"],
  ] as const) {
    test(`${last || "empty"} is ${want}`, () => expect(lastContactText(last, TODAY)).toBe(want));
  }
  test("daysSince never goes below zero", () => expect(daysSince("2030-01-01", TODAY)).toBe(0));
});

test.describe("eventTimeText", () => {
  const now = new Date("2026-10-25T10:00:00Z");
  for (const [ago, want] of [
    [20_000, "just now"],
    [5 * 60_000, "5m ago"],
    [5 * 3600_000, "5h ago"],
    [3 * 86400_000, "3d ago"],
  ] as const) {
    test(want, () => expect(eventTimeText(new Date(now.getTime() - ago), now)).toBe(want));
  }
  test("after a week it is a date", () => {
    expect(eventTimeText(new Date("2026-10-08T10:00:00Z"), now)).toBe("8 Oct");
  });
  test("a time in the future is just now", () => {
    expect(eventTimeText(new Date(now.getTime() + 60_000), now)).toBe("just now");
  });
});

test.describe("relationships", () => {
  test("an unknown or empty relationship is other", () => {
    expect(relationshipOf("")).toBe("other");
    expect(relationshipOf("colleague")).toBe("other");
    expect(relationshipOf("friend")).toBe("friend");
  });
  test("counts are in chip order and leave out empty ones", () => {
    const counts = countByRelationship([
      { relationship: "friend" }, { relationship: "recruiter" }, { relationship: "" }, { relationship: "friend" }, { relationship: "nope" },
    ]);
    expect(counts).toEqual([
      { relationship: "recruiter", count: 1 },
      { relationship: "friend", count: 2 },
      { relationship: "other", count: 2 },
    ]);
  });
  test("initials", () => {
    expect(initialsOf("Priya Raman")).toBe("PR");
    expect(initialsOf("  asha  ")).toBe("A");
    expect(initialsOf("Ana Maria de la Cruz")).toBe("AC");
    expect(initialsOf("   ")).toBe("?");
  });
  test("history text", () => {
    expect(historyText({ kind: "note", text: "hi" })).toBe("hi");
    expect(historyText({ kind: "created", text: "" })).toBe("Added to People");
    expect(historyText({ kind: "wat", text: "" })).toBe("Updated");
  });
});

test.describe("channelFor", () => {
  test("the preferred channel wins", () => {
    expect(channelFor({ preferredChannel: "linkedin", email: "a@b.c" })).toBe(DraftChannel.LINKEDIN);
    expect(channelFor({ preferredChannel: "x", email: "" })).toBe(DraftChannel.X);
    expect(channelFor({ preferredChannel: "phone", email: "" })).toBe(DraftChannel.OTHER);
  });
  test("with none set, an email address means email, and nothing means nothing", () => {
    expect(channelFor({ preferredChannel: "", email: "a@b.c" })).toBe(DraftChannel.EMAIL);
    expect(channelFor({ preferredChannel: "", email: "" })).toBeUndefined();
  });
});
