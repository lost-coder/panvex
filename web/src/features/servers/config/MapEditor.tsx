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
// конфига, поэтому именно здесь сохраняется его ФОРМА. Telemt принимает и
// скаляр, и массив ip:port, и на живой ноде есть обе формы сразу
// (dc_overrides.203 — массив из одного элемента, exclusive_mask — скаляры).
// Свернуть массив из одного элемента в скаляр значило бы переписать конфиг
// при первом же Save и породить ложный дрейф.
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, Input } from "@/ui";

import type { MapValue } from "./containers";

export interface MapEditorProps {
  value: Record<string, MapValue>;
  onChange: (next: Record<string, MapValue>) => void;
  keyLabel: string;
  valueLabel: string;
  disabled?: boolean | undefined;
}

/** Текст поля для значения любой формы. */
function toText(value: MapValue): string {
  return Array.isArray(value) ? value.join(", ") : value;
}

/**
 * Разбирает введённый текст, сохраняя форму значения. Признак списка —
 * ЗАПЯТАЯ В СЫРОМ ТЕКСТЕ, а не количество непустых частей после разбора:
 * иначе едва набранная "1.1.1.1:443," давала бы один элемент, значение
 * оставалось бы скаляром, и запятая исчезала бы из поля прежде, чем
 * оператор успеет набрать второй адрес.
 */
function fromText(text: string, previous: MapValue): MapValue {
  const parts = text.split(",").map((s) => s.trim()).filter(Boolean);
  if (text.includes(",")) return parts;
  if (Array.isArray(previous)) return parts;
  return parts[0] ?? "";
}

export function MapEditor({
  value, onChange, keyLabel, valueLabel, disabled = false,
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
    const taken = rows.some(([key], i) => i !== index && key === nextKey);
    if (taken && nextKey !== "") {
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
    onChange({ ...value, [key]: fromText(text, value[key] ?? "") });
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
        <div key={index} className="flex flex-col gap-1">
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
              className="sm:flex-1"
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
