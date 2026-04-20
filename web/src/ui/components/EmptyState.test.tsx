import { render, screen } from "@testing-library/react";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders title", () => {
    render(<EmptyState title="No data" />);
    expect(screen.getByText("No data")).toBeInTheDocument();
  });

  it("renders description when provided", () => {
    render(<EmptyState title="Empty" description="Nothing here" />);
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
  });

  it("does not render description when omitted", () => {
    const { container } = render(<EmptyState title="Empty" />);
    expect(container.querySelectorAll("p")).toHaveLength(0);
  });

  it("renders default icon", () => {
    render(<EmptyState title="Empty" />);
    expect(screen.getByText("∅")).toBeInTheDocument();
  });

  it("renders custom icon", () => {
    render(<EmptyState title="Empty" icon="🔍" />);
    expect(screen.getByText("🔍")).toBeInTheDocument();
  });

  it("renders action slot", () => {
    render(<EmptyState title="Empty" action={<button>Retry</button>} />);
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("forwards className", () => {
    const { container } = render(<EmptyState title="x" className="custom" />);
    expect(container.firstChild).toHaveClass("custom");
  });
});
