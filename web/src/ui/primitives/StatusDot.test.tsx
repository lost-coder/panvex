import { render, screen } from "@testing-library/react";
import { StatusDot } from "./StatusDot";

describe("StatusDot", () => {
  it("renders with accessible role and label", () => {
    render(<StatusDot status="ok" />);
    const dot = screen.getByRole("img", { name: /status: ok/i });
    expect(dot).toBeInTheDocument();
  });

  it("reflects status in aria-label", () => {
    render(<StatusDot status="error" />);
    expect(screen.getByLabelText("Status: error")).toBeInTheDocument();
  });

  it("applies size classes", () => {
    const { container } = render(<StatusDot status="warn" size="md" />);
    expect(container.firstChild).toHaveClass("h-3", "w-3");
  });

  it("defaults to sm size", () => {
    const { container } = render(<StatusDot status="ok" />);
    expect(container.firstChild).toHaveClass("h-2", "w-2");
  });

  it("forwards className", () => {
    const { container } = render(<StatusDot status="ok" className="extra" />);
    expect(container.firstChild).toHaveClass("extra");
  });
});
