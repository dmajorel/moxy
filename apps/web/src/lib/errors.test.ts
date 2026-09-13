import { describe, expect, it } from "vitest";

import { ApiParseError, ApiRequestError } from "@/api/client";

import { classifyError, explainError } from "./errors";
import type { FailureKind } from "./errors";

describe("classifyError", () => {
  // The statuses internal/server/detail.go actually writes. Telling an
  // operator to check that moxyd is running when moxyd has just answered 404
  // costs an evening.
  it.each([
    [0, "unreachable"],
    [400, "invalid"],
    [401, "unauthorized"],
    [403, "forbidden"],
    [404, "notFound"],
    [501, "unsupported"],
    [502, "upstream"],
    [504, "timeout"],
  ] as const)("reads %s as %s", (status, kind) => {
    expect(classifyError(new ApiRequestError("/api/x", status, null))).toBe(kind);
  });

  // moxyd answered and failed on its own account.
  it.each([500, 503, 599])("reads %s as an internal failure", (status) => {
    expect(classifyError(new ApiRequestError("/api/x", status, null))).toBe("internal");
  });

  // A 4xx we do not know about is not worth a sentence of its own.
  it.each([405, 409, 418, 429])("leaves %s unclassified", (status) => {
    expect(classifyError(new ApiRequestError("/api/x", status, null))).toBe("unknown");
  });

  it("reads a body that is not ours as unreadable", () => {
    const parse = new ApiParseError("GET /api/x returned a body that is not valid JSON");

    expect(classifyError(parse)).toBe("unreadable");
  });

  it("leaves anything that is not an API failure unclassified", () => {
    expect(classifyError(new Error("boom"))).toBe("unknown");
    expect(classifyError(new TypeError("undefined is not a function"))).toBe("unknown");
  });
});

describe("explainError", () => {
  const KINDS: FailureKind[] = [
    "unreachable",
    "unauthorized",
    "notFound",
    "forbidden",
    "upstream",
    "timeout",
    "unsupported",
    "invalid",
    "internal",
    "unreadable",
    "unknown",
  ];

  /** One error that classifies to each kind, so the table is walked whole. */
  const SAMPLES: Record<FailureKind, Error> = {
    unreachable: new ApiRequestError("/api/x", 0, null),
    unauthorized: new ApiRequestError("/api/x", 401, null),
    notFound: new ApiRequestError("/api/x", 404, null),
    forbidden: new ApiRequestError("/api/x", 403, null),
    upstream: new ApiRequestError("/api/x", 502, null),
    timeout: new ApiRequestError("/api/x", 504, null),
    unsupported: new ApiRequestError("/api/x", 501, null),
    invalid: new ApiRequestError("/api/x", 400, null),
    internal: new ApiRequestError("/api/x", 500, null),
    unreadable: new ApiParseError("GET /api/x returned a body that is not valid JSON"),
    unknown: new Error("boom"),
  };

  it.each(KINDS)("gives %s a heading and a whole sentence", (kind) => {
    const explained = explainError(SAMPLES[kind]);

    expect(explained.kind).toBe(kind);
    expect(explained.title).not.toBe("");
    // A body is a sentence that says what to do, not a second heading.
    expect(explained.body).toMatch(/\.$/);
  });

  // Retrying a node that no longer exists will keep failing, while the
  // overview still says what the cluster holds. It is the one failure where
  // the way out is backwards.
  it("offers the way back for a vanished object, and only for it", () => {
    expect(explainError(SAMPLES.notFound).offerBack).toBe(true);

    for (const kind of KINDS.filter((k) => k !== "notFound")) {
      expect(explainError(SAMPLES[kind]).offerBack).toBe(false);
    }
  });

  // The three that used to share one wording are three different errands.
  it("says something different for a deleted VM, a dead cluster and a dead daemon", () => {
    const titles = new Set([
      explainError(SAMPLES.notFound).title,
      explainError(SAMPLES.upstream).title,
      explainError(SAMPLES.unreachable).title,
    ]);

    expect(titles.size).toBe(3);
  });

  // The backend's own message is diagnostic material and is never a label.
  it("never repeats the English message", () => {
    const explained = explainError(new ApiRequestError("/api/x", 404, "not found"));

    expect(explained.title).not.toContain("not found");
    expect(explained.body).not.toContain("not found");
  });
});
