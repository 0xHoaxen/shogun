import { expect, test } from "@playwright/test";

import { canTest, saveReadiness, type Stored, type Typed } from "../src/components/provider/save-rules";

const STORED: Stored = { baseUrl: "https://llm.example.com/v1", model: "my-llama" };
const TYPED: Typed = { baseUrl: "https://llm.example.com/v1", model: "my-llama", apiKey: "sk-key-1" };

test.describe("saveReadiness", () => {
  const cases: { name: string; typed: Typed; tested: Typed | null; stored: Stored | undefined; want: string }[] = [
    { name: "nothing typed and nothing saved", typed: { baseUrl: "", model: "", apiKey: "" }, tested: null, stored: undefined, want: "incomplete" },
    { name: "no key and nothing saved", typed: { ...TYPED, apiKey: "" }, tested: null, stored: undefined, want: "incomplete" },
    { name: "a key but no address", typed: { ...TYPED, baseUrl: "" }, tested: null, stored: undefined, want: "incomplete" },
    { name: "a key but no model", typed: { ...TYPED, model: "  " }, tested: null, stored: undefined, want: "incomplete" },
    { name: "all three typed, never tested", typed: TYPED, tested: null, stored: undefined, want: "needs-test" },
    { name: "tested exactly as typed", typed: TYPED, tested: TYPED, stored: undefined, want: "ready" },
    { name: "tested, spaces around the values do not matter", typed: { baseUrl: " https://llm.example.com/v1 ", model: " my-llama", apiKey: "sk-key-1 " }, tested: TYPED, stored: undefined, want: "ready" },
    { name: "address changed after the test", typed: { ...TYPED, baseUrl: "https://other.example.com" }, tested: TYPED, stored: undefined, want: "needs-test" },
    { name: "model changed after the test", typed: { ...TYPED, model: "another" }, tested: TYPED, stored: undefined, want: "needs-test" },
    { name: "key changed after the test", typed: { ...TYPED, apiKey: "sk-key-2" }, tested: TYPED, stored: undefined, want: "needs-test" },
    { name: "saved provider, nothing changed, no key", typed: { ...STORED, apiKey: "" }, tested: null, stored: STORED, want: "ready" },
    { name: "saved provider, trailing slash and spaces only", typed: { baseUrl: " https://llm.example.com/v1/ ", model: "my-llama", apiKey: "" }, tested: null, stored: STORED, want: "ready" },
    { name: "saved provider, address changed, no key", typed: { baseUrl: "https://other.example.com", model: "my-llama", apiKey: "" }, tested: null, stored: STORED, want: "needs-key" },
    { name: "saved provider, model changed, no key", typed: { baseUrl: STORED.baseUrl, model: "another", apiKey: "" }, tested: null, stored: STORED, want: "needs-key" },
    { name: "saved provider, address changed, key typed, not tested", typed: { ...TYPED, baseUrl: "https://other.example.com" }, tested: null, stored: STORED, want: "needs-test" },
    { name: "saved provider, address changed, key typed and tested", typed: { ...TYPED, baseUrl: "https://other.example.com" }, tested: { ...TYPED, baseUrl: "https://other.example.com" }, stored: STORED, want: "ready" },
    { name: "saved provider, a key typed even for the same details still needs its test", typed: TYPED, tested: null, stored: STORED, want: "needs-test" },
  ];
  for (const c of cases) {
    test(c.name, () => {
      expect(saveReadiness(c.typed, c.tested, c.stored)).toBe(c.want);
    });
  }
});

test.describe("canTest", () => {
  for (const [name, typed, want] of [
    ["all three", TYPED, true],
    ["no key", { ...TYPED, apiKey: "" }, false],
    ["a blank key", { ...TYPED, apiKey: "   " }, false],
    ["no address", { ...TYPED, baseUrl: "" }, false],
    ["no model", { ...TYPED, model: "" }, false],
  ] as const) {
    test(name, () => {
      expect(canTest(typed)).toBe(want);
    });
  }
});
