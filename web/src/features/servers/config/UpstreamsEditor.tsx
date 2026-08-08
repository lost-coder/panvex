// Структурный редактор массива [[upstreams]].
//
// upstreams — массив таблиц, а не набор dotted-путей: раньше 16 записей его
// схемы висели в плоском каталоге мёртвым грузом и подавлялись как
// phantom-строки, а сам массив рендерился как "[object Object]". Схема
// ОДНОГО элемента теперь живёт в UPSTREAM_FIELDS (catalog.upstreamFields),
// и здесь она разворачивается в карточку на каждую запись.
//
// Пустые значения не пишутся в объект записи вовсе (delete, а не ""), чтобы
// Save не порождал ключи, которых в конфиге ноды нет, — иначе дрейф не
// сойдётся, как это было с плоскими ключами до фикса адресации.
//
// F6: карточек несколько, и у каждой одинаковый набор путей полей (type,
// weight, ...), поэтому голого entry.path недостаточно ни для aria-label
// (скринридер читает «weight» дважды, не различая карточки), ни для id/
// htmlFor (два <input> с одним id — невалидный HTML). Оба адресуются
// построчным индексом, как это уже сделано в MapEditor.
import { useId } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Button, Input, Select, Toggle } from "@/ui";

import { UPSTREAM_FIELDS, type ParamCatalogEntry } from "./paramCatalog";
import type { UpstreamEntry } from "./containers";

export interface UpstreamsEditorProps {
  value: UpstreamEntry[];
  onChange: (next: UpstreamEntry[]) => void;
  disabled?: boolean | undefined;
}

// Контролы КОНТРОЛИРУЕМЫЕ (value, не defaultValue): родитель пересевает
// состояние после Save и при переключении на другую ноду, а неуправляемое
// поле показало бы устаревшее значение — ключи карточек (индекс записи и
// путь поля) при таком пересеве не меняются, значит React не перемонтирует
// input и defaultValue уже не применится.
//
// Отсутствующий ключ показывается ЖИВЫМ пустым контролом с дефолтом каталога
// в плейсхолдере — так же, как ConfigTreeField показывает незаданные поля
// дерева. Иначе запись socks5 без ключа password стало бы невозможно
// дополнить: у оператора просто нет другого места, где эти поля задаются.
//
// aria-label — это и доступность (двухколоночная вёрстка разрывает связь
// подписи с контролом, ровно как в ConfigTreeField), и способ однозначно
// адресовать поле: у записи одновременно пусты около десятка полей, и
// искать их по отображаемому значению нельзя.
function EntryControl({
  entry, value, disabled, id, ariaLabel, onChange,
}: Readonly<{
  entry: ParamCatalogEntry;
  value: unknown;
  disabled: boolean;
  id: string;
  ariaLabel: string;
  onChange: (value: unknown) => void;
}>) {
  const placeholder = value === undefined ? entry.default : undefined;

  switch (entry.type) {
    case "boolean":
      return (
        <Toggle
          id={id}
          aria-label={ariaLabel}
          checked={value === true}
          onChange={onChange}
          disabled={disabled}
        />
      );
    case "number":
      return (
        <Input
          id={id}
          aria-label={ariaLabel}
          type="number"
          placeholder={placeholder}
          value={value === undefined || value === null ? "" : String(value)}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
        />
      );
    case "select":
      return (
        <Select
          id={id}
          aria-label={ariaLabel}
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={onChange}
          options={(entry.options ?? []).map((o) => ({ value: o, label: o }))}
        />
      );
    case "string[]":
      return (
        <Input
          id={id}
          aria-label={ariaLabel}
          type="text"
          placeholder={placeholder}
          value={Array.isArray(value) ? value.map(String).join(", ") : ""}
          disabled={disabled}
          onChange={(e) => {
            const list = e.target.value.split(",").map((s) => s.trim()).filter(Boolean);
            onChange(list.length > 0 ? list : undefined);
          }}
        />
      );
    default:
      return (
        <Input
          id={id}
          aria-label={ariaLabel}
          type="text"
          placeholder={placeholder}
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
        />
      );
  }
}

export function UpstreamsEditor({ value, onChange, disabled = false }: Readonly<UpstreamsEditorProps>) {
  const { t } = useTranslation("servers");
  // One id per UpstreamsEditor instance, not one per field: useId() inside
  // the per-field .map() callback would call the hook a variable number of
  // times across renders (entries/fields count changes), which breaks the
  // Rules of Hooks. Suffixing this single id with the row index + field
  // path instead gives every control its own stable, unique id.
  const baseId = useId();

  function updateField(index: number, key: string, next: unknown) {
    const list = value.map((e, i) => {
      if (i !== index) return e;
      const copy = { ...e };
      // Пустое значение = ключа нет, а не ключ с пустой строкой.
      if (next === undefined) delete copy[key];
      else copy[key] = next;
      return copy;
    });
    onChange(list);
  }

  return (
    <div className="flex flex-col gap-4">
      {value.length === 0 && <p className="text-sm text-fg-muted">{t("config.upstreams.empty")}</p>}

      {value.map((upstream, index) => (
        // Порядок в TOML-массиве значим, стабильного id у записи нет —
        // индекс здесь легитимный ключ.
        <div key={index} className="flex flex-col gap-3 rounded-md border border-divider p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm font-medium text-fg">
              {t("config.upstreams.entry", { index: index + 1 })}
            </span>
            <div className="flex items-center gap-2">
              <Badge variant="warn">{t("config.badge.reload")}</Badge>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => onChange(value.filter((_, i) => i !== index))}
              >
                {t("config.upstreams.remove")}
              </Button>
            </div>
          </div>

          <div className="flex flex-col gap-3">
            {UPSTREAM_FIELDS.map((field) => {
              const fieldId = `${baseId}-${index}-${field.path}`;
              return (
                <div
                  key={field.path}
                  className="flex flex-col gap-1.5 sm:grid sm:grid-cols-[minmax(0,1fr)_18rem] sm:items-center sm:gap-4"
                >
                  <label htmlFor={fieldId} className="font-mono text-sm text-fg">
                    {field.path}
                  </label>
                  <EntryControl
                    entry={field}
                    value={upstream[field.path]}
                    disabled={disabled}
                    id={fieldId}
                    ariaLabel={`${field.path} ${index + 1}`}
                    onChange={(next) => updateField(index, field.path, next)}
                  />
                </div>
              );
            })}
          </div>
        </div>
      ))}

      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange([...value, { type: "direct", enabled: true, weight: 1 }])}
        >
          {t("config.upstreams.add")}
        </Button>
      </div>
    </div>
  );
}
