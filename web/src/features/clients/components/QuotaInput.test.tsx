import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { QuotaInput } from "./QuotaInput";

describe("QuotaInput", () => {
  it("displays an existing byte value in the largest readable unit", () => {
    render(<QuotaInput bytes={10 * 1024 ** 3} onBytesChange={vi.fn()} />);
    expect(screen.getByRole("spinbutton", { name: "Data Quota" })).toHaveValue(10);
    expect(screen.getByRole("combobox", { name: "Unit" })).toHaveValue("GB");
  });

  it("commits value + unit as bytes on blur", async () => {
    const onBytesChange = vi.fn();
    render(<QuotaInput bytes={0} onBytesChange={onBytesChange} />);
    await userEvent.type(screen.getByRole("spinbutton", { name: "Data Quota" }), "2");
    await userEvent.tab();
    expect(onBytesChange).toHaveBeenCalledWith(2 * 1024 ** 3);
  });

  it("does not silently wipe the quota when blurred empty", async () => {
    const onBytesChange = vi.fn();
    render(<QuotaInput bytes={1024 ** 3} onBytesChange={onBytesChange} />);
    const input = screen.getByRole("spinbutton", { name: "Data Quota" });
    await userEvent.clear(input);
    await userEvent.tab();
    expect(onBytesChange).not.toHaveBeenCalled();
    expect(input).toHaveValue(1);
  });

  it("rejects negative input (restores previous value)", async () => {
    const onBytesChange = vi.fn();
    render(<QuotaInput bytes={1024 ** 3} onBytesChange={onBytesChange} />);
    const input = screen.getByRole("spinbutton", { name: "Data Quota" });
    await userEvent.clear(input);
    await userEvent.type(input, "-3");
    await userEvent.tab();
    expect(onBytesChange).not.toHaveBeenCalled();
    expect(input).toHaveValue(1);
  });
});
