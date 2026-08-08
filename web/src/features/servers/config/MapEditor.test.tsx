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
//
// valueKind — обязательный проп компонента (F1): форму значения задаёт ТИП
// контейнера в Telemt (dc_overrides — список, censorship.exclusive_mask —
// скаляр), а не то, что оператор набрал. HarnessList/HarnessScalar
// закрепляют это в тестах явно, вместо того чтобы протаскивать valueKind
// как ещё один параметр через каждый вызов.
function Harness({
  initial, onChange, valueKind,
}: Readonly<{
  initial: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
  valueKind: "list" | "scalar";
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
      valueKind={valueKind}
    />
  );
}

function HarnessList({
  initial, onChange,
}: Readonly<{
  initial: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
}>) {
  return <Harness initial={initial} onChange={onChange} valueKind="list" />;
}

function HarnessScalar({
  initial, onChange,
}: Readonly<{
  initial: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
}>) {
  return <Harness initial={initial} onChange={onChange} valueKind="scalar" />;
}

describe("MapEditor", () => {
  it("показывает ключ с точками целиком, не разбивая на сегменты", () => {
    render(
      <MapEditor
        value={{ "hv24s.metrion.icu": "127.0.0.1:8085" }}
        onChange={() => {}}
        keyLabel="SNI"
        valueLabel="Backend"
        valueKind="scalar"
      />,
    );
    expect(screen.getByLabelText("SNI 1")).toHaveValue("hv24s.metrion.icu");
    expect(screen.getByLabelText("Backend 1")).toHaveValue("127.0.0.1:8085");
  });

  // F1, обязательный тест из fixwave-плана: форму значения list-контейнера
  // (dc_overrides) задаёт ТИП контейнера, а не то, сколько адресов оператор
  // набрал — даже первая, единственная запись должна уйти массивом.
  it("новая запись list-контейнера пишется массивом, а не скаляром", async () => {
    const onChange = vi.fn();
    render(<HarnessList initial={{}} onChange={onChange} />);
    await userEvent.click(screen.getByRole("button", { name: /добавить|add/i }));
    await userEvent.type(screen.getByLabelText("K 1"), "204");
    await userEvent.type(screen.getByLabelText("V 1"), "1.2.3.4:443");
    expect(onChange.mock.calls.at(-1)?.[0]["204"]).toEqual(["1.2.3.4:443"]);
  });

  // F1, обязательный тест из fixwave-плана: censorship.exclusive_mask —
  // HashMap<String, String> в Telemt, массив десериализовать не может.
  // Запятая в адресе (порт-лист, второй адрес и т.п.) не должна превращать
  // скаляр в список.
  it("scalar-контейнер не превращает значение в массив из-за запятой", async () => {
    const onChange = vi.fn();
    render(<HarnessScalar initial={{ "a.test": "127.0.0.1:8085" }} onChange={onChange} />);
    await userEvent.type(screen.getByLabelText("V 1"), ",127.0.0.1:8086");
    const last = onChange.mock.calls.at(-1)?.[0]["a.test"];
    expect(Array.isArray(last)).toBe(false);
  });

  it("scalar-контейнер остаётся скаляром после правки в один адрес", async () => {
    const onChange = vi.fn();
    render(<HarnessScalar initial={{ "a.b": "1.1.1.1:443" }} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("V 1"));
    await userEvent.type(screen.getByLabelText("V 1"), "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "a.b": "2.2.2.2:443" });
  });

  // list-контейнер: скаляр из старого конфига читается как есть (readMap
  // переносит форму без изменений), но ЛЮБАЯ правка через редактор отдаёт
  // список — форму задаёт тип контейнера, не то, что было раньше.
  it("list-контейнер отдаёт список после правки, даже если раньше было записано скаляром", async () => {
    const onChange = vi.fn();
    render(<HarnessList initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("V 1"));
    await userEvent.type(screen.getByLabelText("V 1"), "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "203": ["2.2.2.2:443"] });
  });

  it("массив из одного элемента остаётся массивом после правки", async () => {
    const onChange = vi.fn();
    render(<HarnessList initial={{ "203": ["1.1.1.1:443"] }} onChange={onChange} />);
    await userEvent.clear(screen.getByLabelText("V 1"));
    await userEvent.type(screen.getByLabelText("V 1"), "2.2.2.2:443");
    expect(onChange.mock.calls.at(-1)?.[0]).toEqual({ "203": ["2.2.2.2:443"] });
  });

  // Регрессия на съеденную запятую: разбор на каждое нажатие выбрасывал
  // пустой хвост, значение оставалось скаляром, и запятая пропадала из поля
  // раньше, чем оператор успевал набрать второй адрес.
  it("даёт набрать второй адрес посимвольно, не съедая запятую", async () => {
    const onChange = vi.fn();
    render(<HarnessList initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
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
    render(<HarnessList initial={{ "203": "1.1.1.1:443" }} onChange={onChange} />);
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
        valueKind="list"
      />,
    );
    expect(screen.getByLabelText("Endpoints 1")).toHaveValue("91.105.192.100:443, 1.2.3.4:443");
  });

  it("переименование ключа сохраняет значение и его форму", async () => {
    const onChange = vi.fn();
    render(<HarnessList initial={{ "a.b": ["1.1.1.1:443"] }} onChange={onChange} />);
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
      <HarnessScalar
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

  // F5: очистка ключа не должна обходить защиту от коллизии — очистить обе
  // строки подряд (обычный способ переименования) должно оставить обе
  // строки на месте со своими значениями, а не схлопнуть их в одну.
  it("F5: очистка ключей двух строк подряд сохраняет обе строки и показывает сообщение о конфликте", async () => {
    const onChange = vi.fn();
    render(
      <HarnessScalar
        initial={{ "a.b": "1.1.1.1:443", "c.d": "2.2.2.2:443" }}
        onChange={onChange}
      />,
    );
    await userEvent.clear(screen.getByLabelText("K 1"));
    await userEvent.clear(screen.getByLabelText("K 2"));

    expect(screen.getByText(/уже занят|already used/i)).toBeInTheDocument();
    expect(screen.getAllByLabelText(/^K \d/)).toHaveLength(2);
    // Вторая строка не применила очистку (конфликт с уже опустошённой
    // первой), поэтому её значение осталось нетронутым.
    expect(screen.getByLabelText("V 2")).toHaveValue("2.2.2.2:443");
  });

  it("удаляет строку", async () => {
    const onChange = vi.fn();
    render(
      <MapEditor
        value={{ "a.b": "1.1.1.1:443" }}
        onChange={onChange}
        keyLabel="K"
        valueLabel="V"
        valueKind="scalar"
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /удалить|remove/i }));
    expect(onChange).toHaveBeenCalledWith({});
  });

  it("добавляет пустую строку", async () => {
    const onChange = vi.fn();
    render(
      <MapEditor value={{}} onChange={onChange} keyLabel="K" valueLabel="V" valueKind="list" />,
    );
    await userEvent.click(screen.getByRole("button", { name: /добавить|add/i }));
    expect(onChange).toHaveBeenCalledWith({ "": "" });
  });

  // Вторая пустая строка перезаписала бы первую: ключ "" уже занят.
  it("не даёт добавить вторую незаполненную строку", () => {
    render(
      <MapEditor
        value={{ "": "1.2.3.4:443" }}
        onChange={() => {}}
        keyLabel="K"
        valueLabel="V"
        valueKind="scalar"
      />,
    );
    expect(screen.getByRole("button", { name: /добавить|add/i })).toBeDisabled();
  });

  // Позиции строк сдвигаются при удалении: оставленная метка конфликта
  // всплыла бы под непричастной строкой.
  it("снимает сообщение о конфликте при удалении строки", async () => {
    const onChange = vi.fn();
    render(
      <HarnessScalar
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
