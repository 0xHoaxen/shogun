import type { Contact, ContactEvent } from "@/gen/shogun/api/v1/contacts_pb";

// The six ways the owner knows someone (decision D5), in the order the filter
// chips show them.
export const RELATIONSHIPS = ["recruiter", "engineer", "manager", "alumni", "friend", "other"] as const;
export type Relationship = (typeof RELATIONSHIPS)[number];

export const RELATIONSHIP_LABELS: Readonly<Record<Relationship, string>> = {
  recruiter: "Recruiters",
  engineer: "Engineers",
  manager: "Managers",
  alumni: "Alumni",
  friend: "Friends",
  other: "Other",
};

// The singular, for a person's own page and the Add person form.
export const RELATIONSHIP_NAMES: Readonly<Record<Relationship, string>> = {
  recruiter: "Recruiter",
  engineer: "Engineer",
  manager: "Manager",
  alumni: "Alumni",
  friend: "Friend",
  other: "Other",
};

// relationshipOf reads a stored relationship. Anything that is not one of the
// six, including none at all, is "other".
export function relationshipOf(value: string): Relationship {
  return (RELATIONSHIPS as readonly string[]).includes(value) ? (value as Relationship) : "other";
}

export interface RelationshipCount {
  relationship: Relationship;
  count: number;
}

// countByRelationship counts people per relationship, in chip order, leaving
// out the ones nobody has.
export function countByRelationship(contacts: readonly Pick<Contact, "relationship">[]): RelationshipCount[] {
  const counts = new Map<Relationship, number>();
  for (const contact of contacts) {
    const relationship = relationshipOf(contact.relationship);
    counts.set(relationship, (counts.get(relationship) ?? 0) + 1);
  }
  return RELATIONSHIPS.filter((relationship) => counts.has(relationship)).map((relationship) => ({
    relationship,
    count: counts.get(relationship) ?? 0,
  }));
}

// initialsOf is the two letters in a person's square.
export function initialsOf(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  const first = words[0][0];
  const last = words.length > 1 ? words[words.length - 1][0] : "";
  return (first + last).toUpperCase();
}

// historyText is the line a history entry shows. A note shows what was written;
// the other kinds say what happened.
export function historyText(event: Pick<ContactEvent, "kind" | "text">): string {
  switch (event.kind) {
    case "note":
      return event.text;
    case "created":
      return "Added to People";
    case "status_changed":
      return "Status changed";
    case "message_sent":
      return "Message sent";
    case "reply_received":
      return "Reply received";
    default:
      return "Updated";
  }
}
