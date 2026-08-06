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

  it("does not render phantom container catalog rows with no data (upstreams.*, dc_overrides)", () => {
    render(
      <ConfigTree
        desired={{ general: { log_level: "normal" } }}
        observed={{ general: { log_level: "normal" } }}
        groupPaths={new Set()}
        onChange={() => {}}
      />,
    );
    // The generated catalog lists ~16 flat "upstreams.*" sub-paths and a bare
    // "dc_overrides" entry even though the real config nests upstreams as an
    // array, not flat keys. With no desired/observed data for them, buildTree
    // still emits them as readonly TreeFields with value/observed undefined —
    // ConfigTree must suppress those instead of showing blank phantom rows.
    expect(screen.queryByText("address")).not.toBeInTheDocument();
    expect(screen.queryByText("upstreams")).not.toBeInTheDocument();
    expect(screen.queryByText("dc_overrides")).not.toBeInTheDocument();
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
