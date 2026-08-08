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
 * Разбирает введённый текст, сохраняя форму прежнего значения: массив
 * остаётся массивом даже из одного элемента, скаляр остаётся скаляром, а
 * несколько адресов дают массив в любом случае.
 */
function fromText(text: string, previous: MapValue): MapValue {
  const parts = text.split(",").map((s) => s.trim()).filter(Boolean);
  if (parts.length > 1) return parts;
  if (Array.isArray(previous)) return parts;
  return parts[0] ?? "";
}

export function MapEditor({
  value, onChange, keyLabel, valueLabel, disabled = false,
}: Readonly<MapEditorProps>) {
  const { t } = useTranslation("servers");
  const rows = Object.entries(value);

  function renameKey(index: number, nextKey: string) {
    const next: Record<string, MapValue> = {};
    rows.forEach(([key, addresses], i) => {
      next[i === index ? nextKey : key] = addresses;
    });
    onChange(next);
  }

  function setAddresses(key: string, text: string) {
    onChange({ ...value, [key]: fromText(text, value[key] ?? "") });
  }

  return (
    <div className="flex flex-col gap-3">
      {rows.length === 0 && <p className="text-sm text-fg-muted">{t("config.map.empty")}</p>}

      {rows.map(([key, addresses], index) => (
        // Ключ может быть пустым (только что добавленная строка) и меняется
        // при переименовании — позиция здесь стабильнее самого ключа.
        <div key={index} className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Input
            type="text"
            aria-label={keyLabel}
            value={key}
            disabled={disabled}
            onChange={(e) => renameKey(index, e.target.value)}
            className="sm:w-64"
          />
          <Input
            type="text"
            aria-label={valueLabel}
            value={toText(addresses)}
            disabled={disabled}
            onChange={(e) => setAddresses(key, e.target.value)}
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
              onChange(next);
            }}
          >
            {t("config.map.remove")}
          </Button>
        </div>
      ))}

      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange({ ...value, "": "" })}
        >
          {t("config.map.add")}
        </Button>
      </div>
    </div>
  );
}
