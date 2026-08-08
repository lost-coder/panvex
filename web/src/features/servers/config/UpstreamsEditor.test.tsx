import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UpstreamsEditor } from "./UpstreamsEditor";
import type { UpstreamEntry } from "./containers";

// Контролы КОНТРОЛИРУЕМЫЕ, поэтому напечатать в них можно только если
// значение возвращается обратно через value. Harness держит состояние и
// сообщает наружу последнее значение — так тест проверяет реальное поведение
// компонента, а не обходит его, делая поля неуправляемыми.
function Harness({
  initial, onChange,
}: Readonly<{ initial: UpstreamEntry[]; onChange: (next: UpstreamEntry[]) => void }>) {
  const [value, setValue] = useState(initial);
  return (
    <UpstreamsEditor
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange(next);
      }}
    />
  );
}

describe("UpstreamsEditor", () => {
  it("рендерит по карточке на каждый upstream", () => {
    render(<UpstreamsEditor value={[{ type: "direct", weight: 1 }]} onChange={() => {}} />);
    expect(screen.getByLabelText("type")).toHaveValue("direct");
    expect(screen.getByLabelText("weight")).toHaveValue(1);
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
      <Harness
        initial={[{ type: "socks5", address: "1.2.3.4:1080", weight: 3 }]}
        onChange={onChange}
      />,
    );
    await userEvent.clear(screen.getByLabelText("weight"));
    await userEvent.type(screen.getByLabelText("weight"), "5");
    const last = onChange.mock.calls.at(-1)?.[0];
    expect(last[0].weight).toBe(5);
    expect(last[0].type).toBe("socks5");
    expect(last[0].address).toBe("1.2.3.4:1080");
  });

  it("незаданное поле остаётся редактируемым и показывает дефолт плейсхолдером", async () => {
    const onChange = vi.fn();
    render(<Harness initial={[{ type: "socks5" }]} onChange={onChange} />);

    // password на записи нет — но задать его через редактор можно.
    const password = screen.getByLabelText("password");
    expect(password).toHaveValue("");
    await userEvent.type(password, "s3cret");
    expect(onChange.mock.calls.at(-1)?.[0][0].password).toBe("s3cret");

    // weight не задан -> в поле пусто, а дефолт каталога виден плейсхолдером.
    expect(screen.getByLabelText("weight")).toHaveAttribute("placeholder", "1");
  });

  it("очистка поля удаляет ключ, а не пишет пустую строку", async () => {
    const onChange = vi.fn();
    render(<Harness initial={[{ type: "socks5", password: "s3cret" }]} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("password"));
    const last = onChange.mock.calls.at(-1)?.[0];
    expect(last[0]).not.toHaveProperty("password");
    expect(last[0].type).toBe("socks5");
  });

  it("отражает изменение value извне (пересев после Save)", () => {
    const { rerender } = render(
      <UpstreamsEditor value={[{ type: "direct", weight: 1 }]} onChange={() => {}} />,
    );
    rerender(<UpstreamsEditor value={[{ type: "direct", weight: 7 }]} onChange={() => {}} />);
    expect(screen.getByLabelText("weight")).toHaveValue(7);
  });
});
