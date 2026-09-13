import { describe, expect, it } from "vitest";

import { formatPath, parsePath, type Route } from "./routes";

describe("parsePath", () => {
  it("reads the four routes", () => {
    expect(parsePath("/")).toEqual({ kind: "all" });
    expect(parsePath("/clusters/preproduction")).toEqual({
      kind: "cluster",
      clusterId: "preproduction",
    });
    expect(parsePath("/clusters/preproduction/nodes/prox-pprd-2302-cit")).toEqual({
      kind: "node",
      clusterId: "preproduction",
      node: "prox-pprd-2302-cit",
    });
    expect(parsePath("/clusters/preproduction/guests/103")).toEqual({
      kind: "guest",
      clusterId: "preproduction",
      vmid: 103,
    });
  });

  // An operator who types one extra slash should land on the object, not on an
  // error page: the two spellings name the same place.
  it("tolerates a trailing slash", () => {
    expect(parsePath("/clusters/preproduction/")).toEqual(
      parsePath("/clusters/preproduction"),
    );
    expect(parsePath("")).toEqual({ kind: "all" });
    expect(parsePath("///")).toEqual({ kind: "all" });
  });

  it("decodes percent-encoded segments", () => {
    expect(parsePath("/clusters/pr%C3%A9production")).toEqual({
      kind: "cluster",
      clusterId: "préproduction",
    });
    expect(parsePath("/clusters/prod/nodes/node%20one")).toEqual({
      kind: "node",
      clusterId: "prod",
      node: "node one",
    });
  });

  // A path comes from the address bar, which is to say from anyone. The same
  // shapes the backend refuses are refused here, for the same reason.
  it.each([
    ["an unknown first segment", "/dashboard"],
    ["a collection that is not ours", "/clusters/prod/storages/local"],
    ["an empty segment in the middle", "/clusters//nodes/pve-1"],
    ["a missing cluster id", "/clusters"],
    ["a dot segment", "/clusters/./nodes/pve-1"],
    ["a parent segment", "/clusters/../etc/passwd"],
    ["a dot segment as the node", "/clusters/prod/nodes/.."],
    ["one segment too many", "/clusters/prod/nodes/pve-1/rrd"],
    ["one segment too few", "/clusters/prod/nodes"],
    ["a broken percent-encoding", "/clusters/%zz"],
    ["a slash smuggled into a segment", "/clusters/prod%2Fother"],
    ["a relative path", "clusters/prod"],
  ])("refuses %s", (_what, path) => {
    expect(parsePath(path)).toEqual({ kind: "notFound", path });
  });

  // A vmid is a decimal integer in PVE's own range. Everything else would be
  // sent upstream as a path segment, which is how a 500 becomes a mystery.
  it.each([
    ["not a number", "/clusters/prod/guests/abc"],
    ["a float", "/clusters/prod/guests/103.5"],
    ["negative", "/clusters/prod/guests/-1"],
    ["below the PVE minimum", "/clusters/prod/guests/99"],
    ["with a sign", "/clusters/prod/guests/+103"],
    ["with padding", "/clusters/prod/guests/%20103"],
    ["absurdly large", "/clusters/prod/guests/9999999999"],
  ])("refuses a vmid that is %s", (_what, path) => {
    expect(parsePath(path)).toEqual({ kind: "notFound", path });
  });

  it("accepts the bounds of the vmid range", () => {
    expect(parsePath("/clusters/prod/guests/100")).toEqual({
      kind: "guest",
      clusterId: "prod",
      vmid: 100,
    });
    expect(parsePath("/clusters/prod/guests/999999999")).toEqual({
      kind: "guest",
      clusterId: "prod",
      vmid: 999999999,
    });
  });
});

describe("formatPath", () => {
  it("writes the four routes", () => {
    expect(formatPath({ kind: "all" })).toBe("/");
    expect(formatPath({ kind: "cluster", clusterId: "prod" })).toBe("/clusters/prod");
    expect(formatPath({ kind: "node", clusterId: "prod", node: "pve-1" })).toBe(
      "/clusters/prod/nodes/pve-1",
    );
    expect(formatPath({ kind: "guest", clusterId: "prod", vmid: 103 })).toBe(
      "/clusters/prod/guests/103",
    );
  });

  // A node name is operator data. One containing a slash would otherwise
  // become two segments and address something else entirely.
  it("encodes every segment", () => {
    expect(formatPath({ kind: "node", clusterId: "a/b", node: "c d" })).toBe(
      "/clusters/a%2Fb/nodes/c%20d",
    );
  });
});

describe("the two are inverses", () => {
  const routes: Route[] = [
    { kind: "all" },
    { kind: "cluster", clusterId: "preproduction" },
    { kind: "cluster", clusterId: "préproduction" },
    { kind: "node", clusterId: "prod", node: "prox-prod-2401-cit" },
    { kind: "node", clusterId: "prod", node: "node with spaces" },
    { kind: "guest", clusterId: "prod", vmid: 103 },
  ];

  it.each(routes.map((route) => [formatPath(route), route] as const))(
    "round-trips %s",
    (path, route) => {
      expect(parsePath(path)).toEqual(route);
    },
  );

  // The other direction: a path that parses must format back to itself, or a
  // navigation would rewrite the address bar under the operator's cursor.
  it.each([
    "/",
    "/clusters/prod",
    "/clusters/prod/nodes/pve-1",
    "/clusters/prod/guests/103",
  ])("keeps %s unchanged", (path) => {
    expect(formatPath(parsePath(path))).toBe(path);
  });

  // A refused path formats back to itself too: the address bar must keep
  // showing what was typed while the screen says it designates nothing.
  it("leaves an unknown path alone", () => {
    expect(formatPath(parsePath("/nowhere"))).toBe("/nowhere");
  });
});
