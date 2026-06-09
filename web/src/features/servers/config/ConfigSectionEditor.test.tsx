import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConfigSectionEditor } from "./ConfigSectionEditor";

// i18next is bootstrapped globally in vitest.setup.ts, so useTranslation
// resolves the real config.* labels from src/locales/en/servers.json.
describe("ConfigSectionEditor", () => {
  it("renders curated field labels grouped under section headings", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} />);
    // Section heading.
    expect(screen.getByText("General")).toBeInTheDocument();
    // A couple of field labels.
    expect(screen.getByText("Log level")).toBeInTheDocument();
    expect(screen.getByText("Modes")).toBeInTheDocument();
    expect(screen.getByText("SNI domain")).toBeInTheDocument();
  });

  it("renders a Live badge for hot fields and a Restart badge for restart fields", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} />);
    // hot fields (log_level, update_every) -> "Live"
    expect(screen.getAllByText("Live").length).toBeGreaterThan(0);
    // restart fields (modes, tls_domain, tls_domains, client_handshake) -> "Restart"
    expect(screen.getAllByText("Restart").length).toBeGreaterThan(0);
  });

  // The editor is fully controlled (it holds no state), so the rendered
  // input value always reflects the `values` prop. These tests therefore
  // assert the onChange contract for a single change event rather than
  // accumulating keystrokes.
  it("calls onChange(path, value) when a text field is edited", () => {
    const onChange = vi.fn();
    render(
      <ConfigSectionEditor
        values={{ "censorship.tls_domain": "old.com" }}
        onChange={onChange}
      />,
    );
    const input = screen.getByDisplayValue("old.com");
    fireEvent.change(input, { target: { value: "new.com" } });
    expect(onChange).toHaveBeenCalledWith("censorship.tls_domain", "new.com");
  });

  it("emits a number for number fields", () => {
    const onChange = vi.fn();
    render(
      <ConfigSectionEditor
        values={{ "general.update_every": 5 }}
        onChange={onChange}
      />,
    );
    const input = screen.getByDisplayValue("5");
    fireEvent.change(input, { target: { value: "42" } });
    expect(onChange).toHaveBeenCalledWith("general.update_every", 42);
  });

  it("maps string[] fields to/from a comma-separated text input", () => {
    const onChange = vi.fn();
    render(
      <ConfigSectionEditor
        values={{ "censorship.tls_domains": ["a.com", "b.com"] }}
        onChange={onChange}
      />,
    );
    // Array renders as comma-separated text.
    const input = screen.getByDisplayValue("a.com, b.com");
    // A change parses the text back into a trimmed string[].
    fireEvent.change(input, { target: { value: "x.com, y.com" } });
    expect(onChange).toHaveBeenCalledWith("censorship.tls_domains", [
      "x.com",
      "y.com",
    ]);
  });

  it("disables inputs when disabled is set", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} disabled />);
    const inputs = screen.getAllByRole("textbox");
    expect(inputs.length).toBeGreaterThan(0);
    for (const input of inputs) expect(input).toBeDisabled();
  });
});
