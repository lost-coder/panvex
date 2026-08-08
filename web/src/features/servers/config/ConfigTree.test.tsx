import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { ConfigTree } from "./ConfigTree";

describe("ConfigTree", () => {
  it("renders a collapsible section per group", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal" } }}
        observed={{ general: { log_level: "normal" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("general")).toBeInTheDocument();
  });

  it("filters by search text", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal", ad_tag: "x" } }}
        observed={{ general: { log_level: "normal", ad_tag: "x" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "log_level" } });
    expect(screen.getByText("log_level")).toBeInTheDocument();
    expect(screen.queryByText("ad_tag")).not.toBeInTheDocument();
  });

  it("drifted-only filter hides in-sync fields", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal", ad_tag: "x" } }}
        observed={{ general: { log_level: "silent", ad_tag: "x" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /drifted only|только расхождения/i }));
    expect(screen.getByText("log_level")).toBeInTheDocument();
    expect(screen.queryByText("ad_tag")).not.toBeInTheDocument();
  });

  it("does not render rows for container paths absent from both catalog and data (upstreams.*, dc_overrides)", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal" } }}
        observed={{ general: { log_level: "normal" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    // Container split (Task 2): the generated catalog no longer lists
    // upstreams.* / dc_overrides at all — they moved to
    // catalog.upstreamFields / CONTAINER_PATHS (containers.ts), since a flat
    // dotted path through an operator-chosen key (an array index, or a
    // dc_overrides/exclusive_mask key that itself contains dots) is
    // ambiguous. With no catalog entry and no observed data for these paths,
    // buildTree never produces a TreeField for them in the first place, so
    // there is nothing for ConfigTree's phantom-row filter to suppress here
    // — this guards the end state (no blank rows), not the old mechanism.
    expect(screen.queryByText("address")).not.toBeInTheDocument();
    expect(screen.queryByText("upstreams")).not.toBeInTheDocument();
    expect(screen.queryByText("dc_overrides")).not.toBeInTheDocument();
  });

  // F4: buildTree DOES emit a TreeField for a container path when the
  // node's observed config actually reports data there (the comment atop
  // ConfigTree.tsx describes the no-data case only). flattenAll treats an
  // array as a leaf, so `observed.upstreams` becomes one field whose value
  // is the whole array — String([{...}]) renders as the literal text
  // "[object Object]" right above the real UpstreamsEditor below the tree.
  // Each dc_overrides/exclusive_mask entry likewise gets a redundant
  // read-only row in the tree in addition to the editable MapEditor row.
  // ConfigTree must filter CONTAINER_PATHS (and everything nested under
  // them) out of what it renders, regardless of whether buildTree found
  // data for them.
  it("filters out container paths (and their children) even when observed carries data for them", () => {
    render(
      <ConfigTree
        desired={{}}
        observed={{
          general: { log_level: "normal" },
          upstreams: [{ type: "direct", weight: 1 }],
          dc_overrides: { "203": ["1.1.1.1:443"] },
        }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByText("upstreams")).not.toBeInTheDocument();
    expect(screen.queryByText("dc_overrides")).not.toBeInTheDocument();
    expect(screen.queryByText("203")).not.toBeInTheDocument();
    expect(screen.queryByText("[object Object]")).not.toBeInTheDocument();
    // The rest of the tree still renders normally.
    expect(screen.getByText("log_level")).toBeInTheDocument();
  });

  it("changed-only filter hides fields still equal to their initial value", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal", ad_tag: "x" } }}
        observed={{ general: { log_level: "normal", ad_tag: "x" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /changed only|только изменённые/i }));
    expect(screen.queryByText("log_level")).not.toBeInTheDocument();
    expect(screen.queryByText("ad_tag")).not.toBeInTheDocument();
    expect(screen.getByText(/no fields match|нет полей/i)).toBeInTheDocument();
  });

  // P4-T4: threads the per-field drift actions + groupName down to
  // ConfigTreeField for a genuinely drifted field.
  it("threads onAcceptNode/onRevertPanel/groupName down to the drifted field's actions", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal" } }}
        observed={{ general: { log_level: "silent" } }}
        groupPaths={new Set()}
        onChange={() => {}}
        groupName="edge-fleet"
        onAcceptNode={() => {}}
        onRevertPanel={() => {}}
      />,
    );
    expect(screen.getByRole("button", { name: "Accept node value" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revert to panel value" })).toBeInTheDocument();
  });
});
