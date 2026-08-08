import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UpstreamsEditor } from "./UpstreamsEditor";

describe("UpstreamsEditor", () => {
  it("рендерит по карточке на каждый upstream", () => {
    render(<UpstreamsEditor value={[{ type: "direct", weight: 1 }]} onChange={() => {}} />);
    expect(screen.getByDisplayValue("direct")).toBeInTheDocument();
    expect(screen.getByDisplayValue("1")).toBeInTheDocument();
  });

  it("добавляет запись со значением type=direct по умолчанию", async () => {
    const onChange = vi.fn();
    render(<UpstreamsEditor value={[]} onChange={onChange} />);
    await userEvent.click(screen.getByRole("button", { name: /добавить|add/i }));
    expect(onChange).toHaveBeenCalledWith([{ type: "direct", enabled: true, weight: 1 }]);
  });

  it("удаляет запись по индексу", async () => {
    const onChange = vi.fn();
    render(
      <UpstreamsEditor
        value={[{ type: "direct", weight: 1 }, { type: "socks5", weight: 2 }]}
        onChange={onChange}
      />,
    );
    const remove = screen.getAllByRole("button", { name: /удалить|remove/i });
    await userEvent.click(remove[0] as HTMLElement);
    expect(onChange).toHaveBeenCalledWith([{ type: "socks5", weight: 2 }]);
  });

  it("правка поля не затирает соседние ключи записи", async () => {
    const onChange = vi.fn();
    render(
      <UpstreamsEditor value={[{ type: "socks5", address: "1.2.3.4:1080", weight: 3 }]} onChange={onChange} />,
    );
    await userEvent.clear(screen.getByDisplayValue("3"));
    await userEvent.type(screen.getByDisplayValue(""), "5");
    const last = onChange.mock.calls.at(-1)?.[0];
    expect(last[0].type).toBe("socks5");
    expect(last[0].address).toBe("1.2.3.4:1080");
  });

  it("не рендерит пустые необязательные поля как заданные", () => {
    render(<UpstreamsEditor value={[{ type: "direct" }]} onChange={() => {}} />);
    // password не задан -> контрол пуст, а не строка "undefined"
    expect(screen.queryByDisplayValue("undefined")).not.toBeInTheDocument();
  });
});
