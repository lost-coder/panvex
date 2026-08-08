// Редактор таблиц конфига, ключи которых задаёт оператор:
// dc_overrides ("203" -> ip:port) и censorship.exclusive_mask
// ("hv24s.metrion.icu" -> ip:port).
//
// Ключи здесь содержат точки, поэтому адресовать такую запись dotted-путём
// нельзя: "censorship.exclusive_mask.hv24s.metrion.icu" неразличимо
// разбирается на сегменты. Отсюда список пар вместо плоского дерева —
// ключ живёт в собственном input целиком.
//
// Состояние держится в родителе как Record<string, MapValue>; порядок строк
// задаётся порядком ключей объекта, поэтому переименование ключа
// перестраивает объект, сохраняя позицию.
//
// Здесь же — единственное место, где строка из поля превращается в значение
// конфига, поэтому именно здесь решается его ФОРМА. Форму задаёт ТИП
// контейнера в Telemt, а не то, что оператор набрал или что было записано
// раньше:
//   - dc_overrides — HashMap<String, Vec<String>>. Скаляр Telemt примет, но
//     при записи секции обратно (toml::Value::try_from) канонизирует его в
//     массив, поэтому панель обязана слать список — иначе получит из
//     observed массив против своего скаляра и уйдёт в вечный дрейф на этом
//     ключе.
//   - censorship.exclusive_mask — HashMap<String, String>. Массив вообще не
//     десериализуется в это поле — merged.try_into() падает, и Telemt
//     отклоняет ВЕСЬ патч целиком, включая не связанные с этим ключом
//     правки в том же Apply.
// См. valueKind ниже и обязательный проп у вызывающей стороны (ConfigTab).
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, Input } from "@/ui";

import type { MapValue } from "./containers";

export interface MapEditorProps {
  value: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
  keyLabel: string;
  valueLabel: string;
  /**
   * Форму значения задаёт ТИП контейнера в Telemt, а не выбор оператора:
   * dc_overrides — HashMap<String, Vec<String>> (скаляр принимается, но при
   * записи секции обратно канонизируется в массив, поэтому панель обязана
   * слать список); censorship.exclusive_mask — HashMap<String, String>
   * (массив не десериализуется, и Telemt отклоняет ВЕСЬ патч целиком).
   */
  valueKind: "list" | "scalar";
  disabled?: boolean | undefined;
  /**
   * D5: entries the node reports (observed) that `value` (the managed/
   * save-state map) doesn't cover — desired can carry the container with
   * FEWER keys than the node actually has, and F7's whole-container
   * observed-fallback only fires when desired lacks the container
   * ENTIRELY. Rendered read-only with a "take under management" action
   * that folds the entry into `value` via onChange; merely receiving this
   * prop must never itself call onChange — display state stays separate
   * from save state until the operator explicitly acts.
   */
  unmanaged?: Record<string, MapValue> | undefined;
}

/** Текст поля для значения любой формы. */
function toText(value: MapValue): string {
  return Array.isArray(value) ? value.join(", ") : value;
}

/**
 * Разбирает введённый текст в значение нужной формы. Черновик строки
 * (drafts, см. ниже) — отдельная задача: он держит запятую живой под
 * пальцами, пока оператор набирает список; fromText здесь лишь решает,
 * скаляр это или массив, и решает это по valueKind, а не по содержимому
 * текста.
 */
function fromText(text: string, valueKind: "list" | "scalar"): MapValue {
  if (valueKind === "scalar") return text.trim();
  return text.split(",").map((s) => s.trim()).filter(Boolean);
}

export function MapEditor({
  value, onChange, keyLabel, valueLabel, valueKind, disabled = false, unmanaged,
}: Readonly<MapEditorProps>) {
  const { t } = useTranslation("servers");
  const rows = Object.entries(value);

  // Черновик текста строки, пока оператор её редактирует. Контролируемое поле
  // с разбором на каждое нажатие не даёт набрать список: "1.1.1.1:443, " не
  // выживает круг parse→serialize, и пробел с запятой стираются под пальцами.
  // Черновик показывается как есть, наверх при этом уходит уже разобранное
  // значение; на blur черновик снимается, и поле снова следует за value —
  // так внешний пересев (Save, переключение ноды) по-прежнему виден.
  const [drafts, setDrafts] = useState<Record<number, string>>({});

  // Ключ, который оператор пытается ввести, хотя он уже занят другой строкой.
  // Такое переименование не применяется: объект схлопнул бы две строки в одну
  // и молча потерял значение той, что стояла раньше.
  const [conflict, setConflict] = useState<number | null>(null);

  function renameKey(index: number, nextKey: string) {
    // F5: пустой ключ не освобождён от проверки на коллизию. Очистить поле,
    // чтобы набрать заново, — обычный способ переименования; если
    // очистить так две строки подряд, вторая целится в тот же пустой ключ,
    // что и первая, и без этой проверки объект схлопнул бы их в одну,
    // молча потеряв значение первой строки.
    const taken = rows.some(([key], i) => i !== index && key === nextKey);
    if (taken) {
      setConflict(index);
      return;
    }
    setConflict(null);
    const next: Record<string, MapValue> = {};
    rows.forEach(([key, addresses], i) => {
      next[i === index ? nextKey : key] = addresses;
    });
    onChange(next);
  }

  function setAddresses(index: number, key: string, text: string) {
    setDrafts((prev) => ({ ...prev, [index]: text }));
    onChange({ ...value, [key]: fromText(text, valueKind) });
  }

  function commitDraft(index: number) {
    setDrafts((prev) => {
      const next = { ...prev };
      delete next[index];
      return next;
    });
  }

  // Черновики и метка конфликта адресуются позицией строки, а удаление
  // сдвигает позиции: оставленные записи прилипли бы к соседней строке, и
  // оператор увидел бы чужой текст или ошибку «ключ занят» под строкой, где
  // никакого конфликта нет. Структурные изменения снимают и то, и другое.
  function resetRowState() {
    setDrafts({});
    setConflict(null);
  }

  return (
    <div className="flex flex-col gap-3">
      {rows.length === 0 && <p className="text-sm text-fg-muted">{t("config.map.empty")}</p>}

      {rows.map(([key, addresses], index) => (
        // Ключ может быть пустым (только что добавленная строка) и меняется
        // при переименовании — позиция здесь стабильнее самого ключа.
        //
        // D1: max-w-4xl bounds the row the same way ConfigTreeField.tsx:166
        // bounds a tree field row — without it, on a wide screen the row
        // stretched across the whole panel width with ~1100px of dead space
        // between the key and the (previously sm:flex-1, now bounded) value
        // input.
        <div key={index} className="flex max-w-4xl flex-col gap-1">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <Input
              type="text"
              aria-label={`${keyLabel} ${index + 1}`}
              value={key}
              disabled={disabled}
              onChange={(e) => renameKey(index, e.target.value)}
              className="sm:w-64"
            />
            <Input
              type="text"
              aria-label={`${valueLabel} ${index + 1}`}
              value={drafts[index] ?? toText(addresses)}
              disabled={disabled}
              onChange={(e) => setAddresses(index, key, e.target.value)}
              onBlur={() => commitDraft(index)}
              className="sm:w-80"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              onClick={() => {
                const next = { ...value };
                delete next[key];
                resetRowState();
                onChange(next);
              }}
            >
              {t("config.map.remove")}
            </Button>
          </div>
          {conflict === index && (
            <p className="text-caption text-status-error">{t("config.map.duplicateKey")}</p>
          )}
        </div>
      ))}

      {/* D5: entries the node reports that `value` doesn't cover — visible,
          read-only, explicitly marked as node-only. "Take under management"
          folds the entry into `value` via the SAME onChange the managed
          rows use, so it becomes part of what Save persists from that
          click onward; merely rendering this block never calls onChange
          itself. */}
      {unmanaged &&
        Object.entries(unmanaged).map(([key, addresses], index) => (
          <div
            key={`unmanaged-${key}`}
            className="flex max-w-4xl flex-col gap-1 rounded-sm border border-dashed border-divider p-2"
          >
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <Input
                type="text"
                aria-label={`${keyLabel} (${t("config.map.unmanaged.label")}) ${index + 1}`}
                value={key}
                disabled
                className="sm:w-64"
              />
              <Input
                type="text"
                aria-label={`${valueLabel} (${t("config.map.unmanaged.label")}) ${index + 1}`}
                value={toText(addresses)}
                disabled
                className="sm:w-80"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => onChange({ ...value, [key]: addresses })}
              >
                {t("config.map.unmanaged.adopt")}
              </Button>
            </div>
            <p className="text-caption text-fg-muted">{t("config.map.unmanaged.note")}</p>
          </div>
        ))}

      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          // Вторая пустая строка перезаписала бы первую: ключ "" уже занят, и
          // объект схлопнул бы их, потеряв набранное значение.
          disabled={disabled || Object.prototype.hasOwnProperty.call(value, "")}
          onClick={() => {
            resetRowState();
            onChange({ ...value, "": "" });
          }}
        >
          {t("config.map.add")}
        </Button>
      </div>
    </div>
  );
}
