import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MapEditor } from "./MapEditor";
import type { MapValue } from "./containers";

// Поля контролируемые, поэтому напечатать в них можно только если значение
// возвращается обратно через value. Harness держит состояние и сообщает
// наружу последнее значение — так тест проверяет реальное поведение
// компонента, а не обходит его атомарным fireEvent.change.
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
    expect(screen.getByLabelText("SNI 1")).toHaveValue("hv24s.metrion.icu");
    expect(screen.getByLabelText("Backend 1")).toHaveValue("127.0.0.1:8085");
  });

  it("скаляр остаётся скаляром после правки в один адрес", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "a.b": "1.1.1.1:443" }} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("V 1"));
    await userEvent.type(screen.getByLabelText("V 1"), "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "a.b": "2.2.2.2:443" });
  });

  it("массив из одного элемента остаётся массивом после правки", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "203": ["1.1.1.1:443"] }} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("V 1"));
    await userEvent.type(screen.getByLabelText("V 1"), "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "203": ["2.2.2.2:443"] });
  });

  // Регрессия на съеденную запятую: разбор на каждое нажатие выбрасывал
  // пустой хвост, значение оставалось скаляром, и запятая пропадала из поля
  // раньше, чем оператор успевал набрать второй адрес.
  it("даёт набрать второй адрес посимвольно, не съедая запятую", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
    const input = screen.getByLabelText("V 1");

    await userEvent.type(input, ",");
    expect(input).toHaveValue("1.1.1.1:443,");

    await userEvent.type(input, "2.2.2.2:443");
    expect(input).toHaveValue("1.1.1.1:443,2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({
      "203": ["1.1.1.1:443", "2.2.2.2:443"],
    });
  });

  it("после потери фокуса поле показывает нормализованный текст", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
    const input = screen.getByLabelText("V 1");
    await userEvent.type(input, ",2.2.2.2:443");
    await userEvent.tab();
    expect(input).toHaveValue("1.1.1.1:443, 2.2.2.2:443");
  });

  it("несколько адресов показывает списком через запятую", () => {
    render(
      <MapEditor
        value={{ "203": ["91.105.192.100:443", "1.2.3.4:443"] }}
        onChange={() => {}}
        keyLabel="DC" valueLabel="Endpoints"
      />,
    );
    expect(screen.getByLabelText("Endpoints 1")).toHaveValue("91.105.192.100:443, 1.2.3.4:443");
  });

  it("переименование ключа сохраняет значение и его форму", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ "a.b": ["1.1.1.1:443"] }} onChange={onChange} />);
    const keyInput = screen.getByLabelText("K 1");
    await userEvent.clear(keyInput);
    await userEvent.type(keyInput, "c.d");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "c.d": ["1.1.1.1:443"] });
  });

  // Объект схлопнул бы две строки в одну и молча потерял значение той, что
  // стояла раньше, — поэтому переименование в занятый ключ не применяется.
  it("не применяет переименование в уже занятый ключ и сообщает об этом", async () => {
    const onChange = vi.fn();
    render(
      <Harness
        initial={{ "a.b": "1.1.1.1:443", "c.d": "2.2.2.2:443" }}
        onChange={onChange}
      />,
    );
    const keyInput = screen.getByLabelText("K 1");
    await userEvent.clear(keyInput);
    await userEvent.type(keyInput, "c.d");

    expect(screen.getByText(/уже занят|already used/i)).toBeInTheDocument();
    const last = onChange.mock.calls.at(-1)?.[0] ?? {
      "a.b": "1.1.1.1:443",
      "c.d": "2.2.2.2:443",
    };
    expect(last["c.d"]).toBe("2.2.2.2:443");
    expect(Object.keys(last)).toHaveLength(2);
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

  // Вторая пустая строка перезаписала бы первую: ключ "" уже занят.
  it("не даёт добавить вторую незаполненную строку", () => {
    render(
      <MapEditor value={{ "": "1.2.3.4:443" }} onChange={() => {}} keyLabel="K" valueLabel="V" />,
    );
    expect(screen.getByRole("button", { name: /добавить|add/i })).toBeDisabled();
  });

  // Позиции строк сдвигаются при удалении: оставленная метка конфликта
  // всплыла бы под непричастной строкой.
  it("снимает сообщение о конфликте при удалении строки", async () => {
    const onChange = vi.fn();
    render(
      <Harness
        initial={{ "a.b": "1.1.1.1:443", "c.d": "2.2.2.2:443" }}
        onChange={onChange}
      />,
    );
    const keyInput = screen.getByLabelText("K 1");
    await userEvent.clear(keyInput);
    await userEvent.type(keyInput, "c.d");
    expect(screen.getByText(/уже занят|already used/i)).toBeInTheDocument();

    await userEvent.click(screen.getAllByRole("button", { name: /удалить|remove/i })[0] as HTMLElement);
    expect(screen.queryByText(/уже занят|already used/i)).not.toBeInTheDocument();
  });
});
