import { render } from "@testing-library/react";
import { StatusDot } from "./StatusDot";

describe("StatusDot", () => {
  // The dot is purely decorative: callers always render it next to
  // a textual status label, so the dot itself is hidden from
  // assistive tech (aria-hidden=true). Sonar S6819 forbade the
  // <span role="img"> shim that this used to expose.
  it("renders as a decorative element hidden from assistive tech", () => {
    const { container } = render(<StatusDot status="ok" />);
    expect(container.firstChild).toHaveAttribute("aria-hidden", "true");
  });

  it("paints the status color class", () => {
    const { container } = render(<StatusDot status="error" />);
    expect(container.firstChild).toHaveClass("bg-status-error");
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
