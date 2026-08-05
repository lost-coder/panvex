import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConfigSectionEditor } from "./ConfigSectionEditor";

// i18next is bootstrapped globally in vitest.setup.ts, so useTranslation
// resolves the real config.* labels from src/locales/en/servers.json.
describe("ConfigSectionEditor", () => {
  it("renders curated field labels grouped under section headings", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} />);
    // Section heading (still translated via config.section.*).
    expect(screen.getByText("General")).toBeInTheDocument();
    // Task 7: CONFIG_FIELDS now derives from PARAM_CATALOG (the generated
    // catalog) and labelKey carries the raw field key instead of an i18n
    // key with a hand-written English label — t(field.labelKey) falls back
    // to the key itself, so field labels are the raw keys below.
    expect(screen.getByText("log_level")).toBeInTheDocument();
    expect(screen.getByText("ad_tag")).toBeInTheDocument();
    expect(screen.getByText("tls_domain")).toBeInTheDocument();
  });

  it("renders a Live badge for hot fields and a Reload badge for reload fields", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} />);
    // hot fields (log_level, update_every) -> "Live"
    expect(screen.getAllByText("Live").length).toBeGreaterThan(0);
    // reload fields (modes, tls_domain, tls_domains, client_handshake) -> "Reload"
    expect(screen.getAllByText("Reload").length).toBeGreaterThan(0);
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

  // Task 7: censorship.tls_domains is genuinely a Vec<String> in Telemt
  // (src/config/types.rs); the catalog generator now detects the array Type
  // cell and emits "string[]", so FieldInput's string[] branch (comma text
  // <-> array) is reachable again. The field is edited as a comma-separated
  // list but the onChange contract emits a real array, not a joined string.
  it("renders censorship.tls_domains as a comma-separated array field", () => {
    const onChange = vi.fn();
    render(
      <ConfigSectionEditor
        values={{ "censorship.tls_domains": ["a.com", "b.com"] }}
        onChange={onChange}
      />,
    );
    const input = screen.getByDisplayValue("a.com, b.com");
    fireEvent.change(input, { target: { value: "x.com, y.com" } });
    expect(onChange).toHaveBeenCalledWith("censorship.tls_domains", ["x.com", "y.com"]);
  });

  it("associates each field label with its focusable control (a11y)", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} />);
    // The label now carries the field text + apply-mode badge, and its
    // htmlFor resolves to the real control (not the old wrapper <div>).
    // getByLabelText matches on the accessible name (label text content),
    // so "log_level" (the raw key -- see Task 7 note above) resolves the
    // select even though "Live" is appended.
    const select = screen.getByLabelText(/log_level/);
    // The control carries a generated id and the matching <label htmlFor>.
    expect(select.id).toBeTruthy();
    const label = document.querySelector(`label[for="${select.id}"]`);
    expect(label).not.toBeNull();
    expect(label).toHaveTextContent("log_level");
  });

  // The panel only stores what the operator overrode, so on a fresh install
  // `effective` is empty and the form would show no current values at all —
  // the node's real settings live in `observed`. An un-overridden field must
  // therefore fall back to the observed value, which is exactly what the node
  // keeps if the operator leaves the field empty.
  it("falls back to the observed value for the placeholder when nothing is effective", () => {
    render(
      <ConfigSectionEditor
        values={{}}
        observed={{ "censorship.tls_domain": "live.example" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByPlaceholderText("live.example")).toBeInTheDocument();
  });

  it("prefers the effective value over the observed one for the placeholder", () => {
    render(
      <ConfigSectionEditor
        values={{}}
        effective={{ "censorship.tls_domain": "effective.example" }}
        observed={{ "censorship.tls_domain": "live.example" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByPlaceholderText("effective.example")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("live.example")).not.toBeInTheDocument();
  });

  // Selects and toggles have no placeholder, so without an explicit hint a
  // non-overridden field silently misrepresents the node: the select reads
  // "Select…" and the toggle reads OFF even when the node runs something else.
  it("shows the current node value for select fields that have no override", () => {
    render(
      <ConfigSectionEditor
        values={{}}
        observed={{ "general.log_level": "silent" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("Current on node: silent")).toBeInTheDocument();
  });

  it("shows the current node value for toggle fields that have no override", () => {
    render(
      <ConfigSectionEditor
        values={{}}
        observed={{ "general.hardswap": true }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("Current on node: on")).toBeInTheDocument();
  });

  it("omits the current-value hint once the field is overridden", () => {
    render(
      <ConfigSectionEditor
        values={{ "general.log_level": "debug" }}
        observed={{ "general.log_level": "silent" }}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByText("Current on node: silent")).not.toBeInTheDocument();
  });

  it("disables inputs when disabled is set", () => {
    render(<ConfigSectionEditor values={{}} onChange={() => {}} disabled />);
    const inputs = screen.getAllByRole("textbox");
    expect(inputs.length).toBeGreaterThan(0);
    for (const input of inputs) expect(input).toBeDisabled();
  });
});
