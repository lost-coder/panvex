// Структурный редактор массива [[upstreams]].
//
// upstreams — массив таблиц, а не набор dotted-путей: раньше 16 записей его
// схемы висели в плоском каталоге мёртвым грузом и подавлялись как
// phantom-строки, а сам массив рендерился как "[object Object]". Схема
// ОДНОГО элемента теперь живёт в UPSTREAM_FIELDS (catalog.upstreamFields),
// и здесь она разворачивается в карточку на каждую запись: каждое поле
// схемы — своя строка, всегда видимая (полная схема на виду), но живой
// контрол рендерится только для полей, которые реально присутствуют в
// записи (плюс boolean/select — они не участвуют в displayValue-коллизиях
// и всегда предсказуемы). Отсутствующее строковое/числовое поле показывает
// ту же подпись "не задано", что и ConfigTreeField (config.tree.notSet*) —
// это НЕ рендерит его как пустой <input>, что важно и содержательно
// (отсутствие ключа ощутимо отличается от пустой строки), и технически
// (десяток одновременно пустых текстовых input на одной карточке иначе
// неразличимы для getByDisplayValue(""), в т.ч. в реальном UI).
//
// Пустые значения не пишутся в объект записи вовсе (delete, а не ""), чтобы
// Save не порождал ключи, которых в конфиге ноды нет, — иначе дрейф не
// сойдётся, как это было с плоскими ключами до фикса адресации.
import { useTranslation } from "react-i18next";

import { Badge, Button, Input, Select, Toggle } from "@/ui";

import { UPSTREAM_FIELDS, type ParamCatalogEntry } from "./paramCatalog";
import type { UpstreamEntry } from "./containers";

export interface UpstreamsEditorProps {
  value: UpstreamEntry[];
  onChange: (next: UpstreamEntry[]) => void;
  disabled?: boolean | undefined;
}

function EntryControl({
  entry, value, disabled, onChange,
}: Readonly<{
  entry: ParamCatalogEntry;
  value: unknown;
  disabled: boolean;
  onChange: (value: unknown) => void;
}>) {
  switch (entry.type) {
    case "boolean":
      return <Toggle checked={value === true} onChange={onChange} disabled={disabled} />;
    case "number":
      // Uncontrolled: an entry field's value round-trips through the parent
      // only on Save, not on every keystroke, so a `value=` prop here would
      // fight the user's typing (React resets a controlled input back to
      // its last-known prop on every change it doesn't see reflected back).
      return (
        <Input
          type="number"
          defaultValue={value === undefined || value === null ? "" : String(value)}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
        />
      );
    case "select":
      return (
        <Select
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={onChange}
          options={(entry.options ?? []).map((o) => ({ value: o, label: o }))}
        />
      );
    case "string[]":
      return (
        <Input
          type="text"
          defaultValue={Array.isArray(value) ? value.map(String).join(", ") : ""}
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
          type="text"
          defaultValue={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
        />
      );
  }
}

export function UpstreamsEditor({ value, onChange, disabled = false }: Readonly<UpstreamsEditorProps>) {
  const { t } = useTranslation("servers");

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
              const present = field.path in upstream;
              // boolean/select всегда живые: Toggle не участвует в
              // displayValue-сопоставлении, а select "type" на карточке
              // ровно один — коллизии не возникает ни в том, ни в другом
              // случае, поэтому их можно смело показывать даже отсутствующими.
              const alwaysLive = field.type === "boolean" || field.type === "select";
              return (
                <div
                  key={field.path}
                  className="flex flex-col gap-1.5 sm:grid sm:grid-cols-[minmax(0,1fr)_18rem] sm:items-center sm:gap-4"
                >
                  <label className="font-mono text-sm text-fg">{field.path}</label>
                  {present || alwaysLive ? (
                    <EntryControl
                      entry={field}
                      value={upstream[field.path]}
                      disabled={disabled}
                      onChange={(next) => updateField(index, field.path, next)}
                    />
                  ) : (
                    <p className="text-caption text-fg-muted">
                      {field.default !== undefined
                        ? t("config.tree.notSetWithDefault", { value: field.default })
                        : t("config.tree.notSet")}
                    </p>
                  )}
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
