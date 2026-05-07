import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RestartBanner } from "./RestartBanner";

describe("RestartBanner", () => {
  it("renders nothing when pendingFields is empty", () => {
    const { container } = render(
      <RestartBanner pendingFields={[]} onRestart={vi.fn()} restartInFlight={false} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("shows count and field names when pendingFields is non-empty", () => {
    render(
      <RestartBanner
        pendingFields={["foo", "bar"]}
        onRestart={vi.fn()}
        restartInFlight={false}
      />,
    );
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText(/foo, bar/)).toBeInTheDocument();
  });

  it("shows singular grammar for one field", () => {
    render(
      <RestartBanner
        pendingFields={["only.one"]}
        onRestart={vi.fn()}
        restartInFlight={false}
      />,
    );
    expect(screen.getByText(/setting needs/)).toBeInTheDocument();
  });

  it("shows plural grammar for multiple fields", () => {
    render(
      <RestartBanner
        pendingFields={["a", "b"]}
        onRestart={vi.fn()}
        restartInFlight={false}
      />,
    );
    expect(screen.getByText(/settings need/)).toBeInTheDocument();
  });

  it("button visible and calls onRestart on click", async () => {
    const onRestart = vi.fn();
    render(
      <RestartBanner
        pendingFields={["x.y"]}
        onRestart={onRestart}
        restartInFlight={false}
      />,
    );
    const user = userEvent.setup();
    const btn = screen.getByRole("button", { name: /Restart now/i });
    expect(btn).toBeInTheDocument();
    await user.click(btn);
    expect(onRestart).toHaveBeenCalledTimes(1);
  });

  it("button disabled and shows Restarting… when restartInFlight=true", () => {
    render(
      <RestartBanner
        pendingFields={["x.y"]}
        onRestart={vi.fn()}
        restartInFlight={true}
      />,
    );
    const btn = screen.getByRole("button", { name: /Restarting…/i });
    expect(btn).toBeDisabled();
  });
});
