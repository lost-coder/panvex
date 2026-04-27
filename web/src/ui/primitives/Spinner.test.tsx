import { render, screen } from "@testing-library/react";
import { Spinner } from "./Spinner";

describe("Spinner", () => {
  it("renders with status role", () => {
    render(<Spinner />);
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("has accessible label", () => {
    render(<Spinner />);
    expect(screen.getByLabelText("Loading")).toBeInTheDocument();
  });

  it("renders an svg", () => {
    render(<Spinner />);
    // The status role is on the <output> wrapper (Sonar S6819 fix). The svg
    // it wraps is aria-hidden, so query through the live DOM directly.
    const svg = screen.getByRole("status").querySelector("svg");
    expect(svg).not.toBeNull();
  });

  it("forwards className", () => {
    render(<Spinner className="extra" />);
    // className is forwarded to the svg, not the <output> shell.
    const svg = screen.getByRole("status").querySelector("svg");
    expect(svg).toHaveClass("extra");
  });
});
