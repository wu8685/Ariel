import { describe, expect, it } from "vitest";
import { answersForSubmission } from "./interaction";
import type { Interaction } from "./generated/protocol";

describe("interaction answers", () => {
  const card = { interactionId: "req:one", kind: "user_input", prompt: "", availableDecisions: ["answer"], questions: [{ id: "choice", question: "Choose", options: ["Blue", "Green"] }, { id: "note", question: "Explain", options: ["Preset"] }] } as Interaction;

  it("keeps an offered option and an explicit free-text answer", () => {
    expect(answersForSubmission(card, { choice: "Blue", note: "my own answer" })).toEqual({ choice: ["Blue"], note: ["my own answer"] });
  });

  it("rejects missing or whitespace-only answers", () => {
    expect(answersForSubmission(card, { choice: "Blue" })).toBeNull();
    expect(answersForSubmission(card, { choice: "Blue", note: "  " })).toBeNull();
  });
});
