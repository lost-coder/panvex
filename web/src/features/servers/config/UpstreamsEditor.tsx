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
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Button, Input, Select, Toggle } from "@/ui";

import { UPSTREAM_FIELDS, type ParamCatalogEntry } from "./paramCatalog";
import type { UpstreamEntry } from "./containers";

// D2: at type=direct, only a handful of the 16 schema fields are
// meaningful — the rest (username/password/address for SOCKS, url for
// Shadowsocks, user_id for SOCKS4) are dead weight on screen for the most
// common configuration and were what made the owner call the form "huge and
// uncomfortable" on live review. ALWAYS_VISIBLE_FIELDS applies regardless of
// type; TYPE_VISIBLE_FIELDS adds the ones relevant to the selected type.
// Anything left over is still reachable (an operator may legitimately set
// any field) behind a "show remaining fields" disclosure — never hidden for
// good.
const ALWAYS_VISIBLE_FIELDS = new Set([
  "type", "enabled", "weight", "scopes", "prefer", "ipv4", "ipv6",
  "interface", "bindtodevice", "bind_addresses", "force_bind",
]);

const TYPE_VISIBLE_FIELDS: Record<string, readonly string[]> = {
  socks4: ["address", "user_id"],
  socks5: ["address", "username", "password"],
  shadowsocks: ["url"],
  direct: [],
};

function splitFieldsByRelevance(
  type: unknown,
): { primary: ParamCatalogEntry[]; secondary: ParamCatalogEntry[] } {
  const extra = TYPE_VISIBLE_FIELDS[typeof type === "string" ? type : ""] ?? [];
  const visible = new Set([...ALWAYS_VISIBLE_FIELDS, ...extra]);
  const primary: ParamCatalogEntry[] = [];
  const secondary: ParamCatalogEntry[] = [];
  for (const field of UPSTREAM_FIELDS) {
    (visible.has(field.path) ? primary : secondary).push(field);
  }
  return { primary, secondary };
}

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

// D1 + D3: max-w-4xl bounds the row the same way ConfigTreeField.tsx:166
// bounds a tree field row (without it the label sat pinned left, the
// control pinned right, with ~1100px of dead space between on a wide
// screen); D3 lays the set of rows out in two columns on sm+ so a
// shortened, still-vertical card isn't stretched down the page.
function FieldRow({
  field, value, disabled, id, ariaLabel, onChange,
}: Readonly<{
  field: ParamCatalogEntry;
  value: unknown;
  disabled: boolean;
  id: string;
  ariaLabel: string;
  onChange: (value: unknown) => void;
}>) {
  return (
    <div className="flex max-w-4xl flex-col gap-1.5 sm:grid sm:grid-cols-[minmax(0,1fr)_18rem] sm:items-center sm:gap-4">
      <label htmlFor={id} className="font-mono text-sm text-fg">
        {field.path}
      </label>
      <EntryControl
        entry={field}
        value={value}
        disabled={disabled}
        id={id}
        ariaLabel={ariaLabel}
        onChange={onChange}
      />
    </div>
  );
}

export function UpstreamsEditor({ value, onChange, disabled = false }: Readonly<UpstreamsEditorProps>) {
  const { t } = useTranslation("servers");
  // One id per UpstreamsEditor instance, not one per field: useId() inside
  // the per-field .map() callback would call the hook a variable number of
  // times across renders (entries/fields count changes), which breaks the
  // Rules of Hooks. Suffixing this single id with the row index + field
  // path instead gives every control its own stable, unique id.
  const baseId = useId();
  // D2: which cards have their "remaining fields" disclosure open, keyed by
  // row index. A real conditional render (not a native <details>, whose
  // closed content stays present in the DOM — just visually hidden, so it
  // wouldn't actually shorten the form) so the secondary fields genuinely
  // aren't rendered until the operator asks for them.
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  function toggleExpanded(index: number) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }

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

      {value.map((upstream, index) => {
        // D2: which fields are relevant depends on THIS entry's own type —
        // recomputed per card, since a form can mix a socks5 upstream and a
        // direct one.
        const { primary, secondary } = splitFieldsByRelevance(upstream["type"]);
        const fieldRow = (field: ParamCatalogEntry) => (
          <FieldRow
            key={field.path}
            field={field}
            value={upstream[field.path]}
            disabled={disabled}
            id={`${baseId}-${index}-${field.path}`}
            ariaLabel={`${field.path} ${index + 1}`}
            onChange={(next) => updateField(index, field.path, next)}
          />
        );
        return (
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

            {/* D3: two columns on sm+, one on narrow — each row stays its
                own label/control pair via FieldRow's own inner grid. */}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-x-6 sm:gap-y-3">
              {primary.map(fieldRow)}
            </div>

            {/* D2: fields irrelevant to the selected type are reachable,
                never hidden for good — just collapsed by default so the
                common case (type=direct) isn't a 16-field wall. A real
                conditional render, not a native <details> (whose closed
                content stays in the DOM, just visually hidden — it
                wouldn't actually shorten the form for anyone using it). */}
            {secondary.length > 0 && (
              <div className="rounded-sm border border-dashed border-divider p-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-expanded={expanded.has(index)}
                  onClick={() => toggleExpanded(index)}
                >
                  {t("config.upstreams.showRemaining")}
                </Button>
                {expanded.has(index) && (
                  <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-x-6 sm:gap-y-3">
                    {secondary.map(fieldRow)}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}

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
