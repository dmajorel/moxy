import { describe, expect, it } from "vitest";

import type { Guest } from "@/api/types";
import { guestIndicator, isTroubledHaState } from "./guestState";

function guest(patch: Partial<Guest> = {}): Guest {
  return {
    vmid: 101,
    name: "sli-airflow-sep-exp-2601-qul",
    kind: "qemu",
    status: "running",
    cpu: { ratio: 0.1, cores: 4 },
    memory: { used: 1, total: 2, ratio: 0.5 },
    tags: [],
    haState: null,
    agent: null,
    ...patch,
  };
}

describe("isTroubledHaState", () => {
  it("counts the two states nobody chose", () => {
    expect(isTroubledHaState("error")).toBe(true);
    expect(isTroubledHaState("fence")).toBe(true);
  });

  // A planned move is the manager doing its job. Painting it red would cry
  // wolf through every maintenance window, which is how a colour stops being
  // read at all.
  it("leaves the CRM's ordinary work alone", () => {
    for (const state of ["started", "stopped", "disabled", "ignored", "migrate", "relocate", "freeze"]) {
      expect(isTroubledHaState(state)).toBe(false);
    }
  });

  it("treats no HA state as no incident", () => {
    expect(isTroubledHaState(null)).toBe(false);
  });
});

describe("guestIndicator", () => {
  it("reports the runtime state when nothing else applies", () => {
    expect(guestIndicator(guest({ status: "running" }))).toBe("running");
    expect(guestIndicator(guest({ status: "stopped" }))).toBe("stopped");
    expect(guestIndicator(guest({ status: "template" }))).toBe("template");
  });

  it("says agentless of a running VM whose agent is not configured", () => {
    expect(guestIndicator(guest({ status: "running", agent: false }))).toBe("agentless");
  });

  // Unknown is not "no". A VM the sweep has not reached, a container that has
  // no such agent, an older PVE that omits the field: all three are null, and
  // none of them is a VM without an agent.
  it("never says agentless of an unknown agent", () => {
    expect(guestIndicator(guest({ status: "running", agent: null }))).toBe("running");
    expect(guestIndicator(guest({ kind: "lxc", status: "running", agent: null }))).toBe("running");
  });

  // A stopped machine has no agent answering either way, so the question does
  // not arise: flagging it would mark half a fleet for a fact that says
  // nothing about it.
  it("only asks the agent question of a running VM", () => {
    expect(guestIndicator(guest({ status: "stopped", agent: false }))).toBe("stopped");
  });

  it("lets an incident win over everything else", () => {
    expect(guestIndicator(guest({ status: "running", haState: "error" }))).toBe("troubled");
    // Including over a state that would otherwise be shown instead.
    expect(guestIndicator(guest({ status: "stopped", haState: "fence" }))).toBe("troubled");
    expect(guestIndicator(guest({ status: "running", agent: false, haState: "error" }))).toBe(
      "troubled",
    );
    expect(guestIndicator(guest({ status: "template", haState: "error" }))).toBe("troubled");
  });

  it("does not turn an ordinary CRM state into an incident", () => {
    expect(guestIndicator(guest({ status: "running", haState: "started" }))).toBe("running");
    expect(guestIndicator(guest({ status: "running", agent: false, haState: "started" }))).toBe(
      "agentless",
    );
  });
});
