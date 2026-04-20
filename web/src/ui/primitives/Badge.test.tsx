import { render, screen } from "@testing-library/react";
import { Badge } from "./Badge";

describe("Badge", () => {
  it("renders children", () => {
    render(<Badge>Online</Badge>);
    expect(screen.getByText("Online")).toBeInTheDocument();
  });

  it("applies default variant classes", () => {
    const { container } = render(<Badge>tag</Badge>);
    expect(container.firstChild).toHaveClass("inline-flex");
  });

  it("accepts variant prop", () => {
    render(<Badge variant="ok">OK</Badge>);
    expect(screen.getByText("OK")).toBeInTheDocument();
  });

  it("forwards className", () => {
    render(<Badge className="custom-class">x</Badge>);
    expect(screen.getByText("x")).toHaveClass("custom-class");
  });

  it("renders as a span", () => {
    render(<Badge>s</Badge>);
    expect(screen.getByText("s").tagName).toBe("SPAN");
  });
});
