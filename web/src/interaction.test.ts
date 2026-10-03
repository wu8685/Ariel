import { describe, expect, it } from "vitest";
import { answersForSubmission } from "./interaction";

describe("interaction answers", () => {
  const card = { interactionId: "req:one", kind: "user_input" as const, prompt: "", availableDecisions: ["answer" as const], questions: [{ id: "choice", question: "Choose", options: ["Blue", "Green"] }, { id: "note", question: "Explain", options: ["Preset"] }] };

  it("keeps an offered option and an explicit free-text answer", () => {
    expect(answersForSubmission(card, { choice: "Blue", note: "my own answer" })).toEqual({ choice: ["Blue"], note: ["my own answer"] });
  });

  it("rejects missing or whitespace-only answers", () => {
    expect(answersForSubmission(card, { choice: "Blue" })).toBeNull();
    expect(answersForSubmission(card, { choice: "Blue", note: "  " })).toBeNull();
  });
});
