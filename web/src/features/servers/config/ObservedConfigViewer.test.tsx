import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ObservedConfigViewer } from "./ObservedConfigViewer";

// i18next is bootstrapped globally in vitest.setup.ts, so useTranslation
// resolves the real config.observed.* labels from src/locales/en/servers.json.
describe("ObservedConfigViewer", () => {
  it("renders observed keys not covered by the editor, flagging process-owned", () => {
    render(
      <ObservedConfigViewer
        observed={{ general: { data_path: "/x", ntp_check: true } }}
      />,
    );
    // process-owned field: not editable, shown with its restart note.
    expect(screen.getByText("general.data_path")).toBeInTheDocument();
    expect(screen.getByText(/перезапуск|restart/i)).toBeInTheDocument();
    // general.ntp_check IS in CONFIG_FIELDS (editable above) -> excluded here.
    expect(screen.queryByText("general.ntp_check")).not.toBeInTheDocument();
  });

  it("excludes every CONFIG_FIELDS path, keeping unmanaged sections", () => {
    render(
      <ObservedConfigViewer
        observed={{
          general: { log_level: "info" },
          upstreams: [{ dc: 1, host: "1.2.3.4" }],
        }}
      />,
    );
    expect(screen.queryByText("general.log_level")).not.toBeInTheDocument();
    expect(screen.getByText("upstreams")).toBeInTheDocument();
  });

  it("shows an empty state when nothing is left to display", () => {
    render(<ObservedConfigViewer observed={{ general: { log_level: "info" } }} />);
    expect(
      screen.getByText(/no additional fields|нет дополнительных/i),
    ).toBeInTheDocument();
  });

  it("renders nothing extra for a normal (non process-owned) observed field", () => {
    render(<ObservedConfigViewer observed={{ access: { max_clients: 10 } }} />);
    expect(screen.getByText("access.max_clients")).toBeInTheDocument();
    expect(screen.getByText("10")).toBeInTheDocument();
  });
});
