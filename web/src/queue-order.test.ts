import { describe, expect, it } from "vitest";
import { reorderQueueAtPointer } from "./queue-order";

const order = ["queue-one", "queue-two", "queue-three"];
const rows = [
  { queueId: "queue-one", top: 0, bottom: 50 },
  { queueId: "queue-two", top: 50, bottom: 100 },
  { queueId: "queue-three", top: 100, bottom: 150 },
];

describe("queue pointer ordering", () => {
  it("moves the first item below every row when the pointer passes the final midpoint", () => {
    expect(reorderQueueAtPointer(order, "queue-one", 140, rows)).toEqual(["queue-two", "queue-three", "queue-one"]);
  });

  it("moves the final item above every row when the pointer passes the first midpoint", () => {
    expect(reorderQueueAtPointer(order, "queue-three", -10, rows)).toEqual(["queue-three", "queue-one", "queue-two"]);
  });

  it("keeps the order when the pointer has not crossed an adjacent midpoint", () => {
    expect(reorderQueueAtPointer(order, "queue-one", 60, rows)).toEqual(order);
  });
});
