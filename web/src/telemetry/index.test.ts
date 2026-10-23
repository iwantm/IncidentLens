import { describe, expect, it } from "vitest";
import { initializeTelemetry, recordNavigation } from "./index";

describe("frontend telemetry without an exporter", () => {
  it("keeps telemetry optional when no OTLP endpoint is configured", () => {
    expect(() => initializeTelemetry()).not.toThrow();
    expect(() => recordNavigation("/incidents")).not.toThrow();
  });
});
