import type { Interaction } from "./generated/protocol";

export function answersForSubmission(card: Interaction, values: Record<string, string>): Record<string, string[]> | null {
  if (card.kind !== "user_input" || !card.questions?.length) return null;
  const answers: Record<string, string[]> = {};
  for (const question of card.questions) {
    const value = values[question.id];
    if (!value?.trim()) return null;
    answers[question.id] = [value];
  }
  return answers;
}
