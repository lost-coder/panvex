import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { TelemtUnreachableBanner } from "./TelemtUnreachableBanner";

describe("TelemtUnreachableBanner", () => {
  it("renders the unreachable-since timestamp and elapsed duration", () => {
    const since = 1700000000; // 2023-11-14T22:13:20Z
    const now = since + 90; // 90 seconds elapsed
    render(<TelemtUnreachableBanner sinceUnix={since} nowUnix={now} />);

    expect(screen.getByRole("alert")).toHaveTextContent(/Telemt connection lost/i);
    expect(screen.getByRole("alert")).toHaveTextContent("1m");
    expect(screen.getByRole("alert")).toHaveTextContent("22:13:20");
  });

  it("falls back to a neutral message when sinceUnix is missing", () => {
    render(<TelemtUnreachableBanner sinceUnix={0} nowUnix={Date.now() / 1000} />);
    expect(screen.getByRole("alert")).toHaveTextContent(/Telemt connection lost/i);
  });
});
