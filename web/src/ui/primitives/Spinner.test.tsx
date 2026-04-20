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
    expect(screen.getByRole("status").tagName).toBe("svg");
  });

  it("forwards className", () => {
    render(<Spinner className="extra" />);
    expect(screen.getByRole("status")).toHaveClass("extra");
  });
});
