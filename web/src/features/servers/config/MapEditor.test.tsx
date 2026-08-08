import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MapEditor } from "./MapEditor";
import type { MapValue } from "./containers";

// Контролы КОНТРОЛИРУЕМЫЕ (см. UpstreamsEditor.test.tsx), поэтому напечатать
// в них можно только если значение возвращается обратно через value —
// иначе React откатывает DOM к прежнему value после каждого нажатия. Harness
// держит состояние и сообщает наружу последнее значение, так тест проверяет
// реальное поведение компонента, а не обходит его.
function Harness({
  initial, onChange,
}: Readonly<{
  initial: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
}>) {
  const [value, setValue] = useState(initial);
  return (
    <MapEditor
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange(next);
      }}
      keyLabel="K"
      valueLabel="V"
    />
  );
}

describe("MapEditor", () => {
  it("показывает ключ с точками целиком, не разбивая на сегменты", () => {
    render(
      <MapEditor
        value={{ "hv24s.metrion.icu": "127.0.0.1:8085" }}
        onChange={() => {}}
        keyLabel="SNI"
        valueLabel="Backend"
      />,
    );
    expect(screen.getByDisplayValue("hv24s.metrion.icu")).toBeInTheDocument();
    expect(screen.getByDisplayValue("127.0.0.1:8085")).toBeInTheDocument();
  });

  it("скаляр остаётся скаляром после правки в один адрес", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "a.b": "1.1.1.1:443" }} onChange={onChange} />);
    const valueInput = screen.getByDisplayValue("1.1.1.1:443");
    await userEvent.clear(valueInput);
    await userEvent.type(valueInput, "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "a.b": "2.2.2.2:443" });
  });

  it("массив из одного элемента остаётся массивом после правки", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "203": ["1.1.1.1:443"] }} onChange={onChange} />);
    const valueInput = screen.getByDisplayValue("1.1.1.1:443");
    await userEvent.clear(valueInput);
    await userEvent.type(valueInput, "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "203": ["2.2.2.2:443"] });
  });

  it("скаляр становится массивом, когда адресов больше одного", () => {
    // Одно атомарное изменение (как при вставке или быстром наборе), а не
    // посимвольный userEvent.type: React откатывает контролируемый input к
    // последнему value между каждым нажатием, а на полпути к запятой без
    // второго адреса fromText ещё не видит второй элемент и возвращает
    // прежний скаляр без изменений — DOM откатывается и запятая теряется
    // ДО того, как успевает набраться второй адрес. Само событие onChange
    // получает полный текст независимо от того, как он туда попал.
    const onChange = vi.fn();
    render(<Harness initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
    const valueInput = screen.getByDisplayValue("1.1.1.1:443");
    fireEvent.change(valueInput, { target: { value: "1.1.1.1:443, 2.2.2.2:443" } });
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({
      "203": ["1.1.1.1:443", "2.2.2.2:443"],
    });
  });

  it("несколько адресов показывает списком через запятую", () => {
    render(
      <MapEditor
        value={{ "203": ["91.105.192.100:443", "1.2.3.4:443"] }}
        onChange={() => {}}
        keyLabel="DC" valueLabel="Endpoints"
      />,
    );
    expect(screen.getByDisplayValue("91.105.192.100:443, 1.2.3.4:443")).toBeInTheDocument();
  });

  it("переименование ключа сохраняет значение и его форму", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "a.b": ["1.1.1.1:443"] }} onChange={onChange} />);
    const keyInput = screen.getByDisplayValue("a.b");
    await userEvent.clear(keyInput);
    await userEvent.type(keyInput, "c.d");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "c.d": ["1.1.1.1:443"] });
  });

  it("удаляет строку", async () => {
    const onChange = vi.fn();
    render(
      <MapEditor value={{ "a.b": "1.1.1.1:443" }} onChange={onChange} keyLabel="K" valueLabel="V" />,
    );
    await userEvent.click(screen.getByRole("button", { name: /удалить|remove/i }));
    expect(onChange).toHaveBeenCalledWith({});
  });

  it("добавляет пустую строку", async () => {
    const onChange = vi.fn();
    render(<MapEditor value={{}} onChange={onChange} keyLabel="K" valueLabel="V" />);
    await userEvent.click(screen.getByRole("button", { name: /добавить|add/i }));
    expect(onChange).toHaveBeenCalledWith({ "": "" });
  });
});
