import { useMemo, useState } from "react";

import {
  filterAssignmentOptions,
  summarizeAssignmentSelection,
  type AssignmentOption,
  type AssignmentSummary
} from "../clients-form-state";

type ClientAssignmentPickerProps = {
  title: string;
  description: string;
  searchPlaceholder: string;
  options: AssignmentOption[];
  selected: string[];
  onToggle: (id: string) => void;
};

type ClientAssignmentSummaryProps = {
  summary: AssignmentSummary;
};

export function ClientAssignmentPicker(props: ClientAssignmentPickerProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState("");

  const selectedOptions = useMemo(
    () => props.options.filter((option) => props.selected.includes(option.id)),
    [props.options, props.selected]
  );

  const filteredOptions = useMemo(
    () => filterAssignmentOptions(props.options, query, props.selected),
    [props.options, query, props.selected]
  );

  const visibleOptions = query.trim() === "" ? filteredOptions.slice(0, 8) : filteredOptions.slice(0, 20);
  const hiddenCount = filteredOptions.length - visibleOptions.length;

  return (
    <>
      <button
        type="button"
        className="w-full rounded-3xl border border-slate-200 bg-slate-50 px-4 py-4 text-left transition hover:border-slate-300 hover:bg-slate-100"
        onClick={() => setIsOpen(true)}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-medium text-slate-900">{props.title}</p>
            <p className="mt-1 text-xs leading-5 text-slate-500">{props.description}</p>
            <p className="mt-3 truncate text-sm text-slate-700">{summarizeAssignmentSelection(props.options, props.selected)}</p>
          </div>
          <span className="shrink-0 rounded-full bg-slate-950 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-white">
            {props.selected.length}
          </span>
        </div>
      </button>

      {isOpen ? (
        <div className="fixed inset-0 z-50 bg-slate-950/45 px-4 py-4 sm:flex sm:items-center sm:justify-center sm:p-6">
          <div className="flex h-full w-full flex-col rounded-[28px] border border-white/70 bg-white shadow-[0_28px_80px_rgba(15,23,42,0.24)] sm:h-auto sm:max-h-[85vh] sm:max-w-2xl">
            <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-5">
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.title}</p>
                <p className="mt-2 text-sm leading-6 text-slate-600">{props.description}</p>
              </div>
              <button
                type="button"
                className="rounded-2xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs font-medium text-slate-700 transition hover:bg-slate-100"
                onClick={() => setIsOpen(false)}
              >
                Close
              </button>
            </div>

            <div className="flex-1 overflow-y-auto px-5 py-5">
              <input
                type="text"
                value={query}
                placeholder={props.searchPlaceholder}
                className="w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900"
                onChange={(event) => setQuery(event.target.value)}
              />

              {selectedOptions.length > 0 ? (
                <div className="mt-4">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-500">Selected</p>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {selectedOptions.map((option) => (
                      <button
                        key={option.id}
                        type="button"
                        className="inline-flex items-center gap-2 rounded-full bg-slate-950 px-3 py-2 text-xs font-medium text-white transition hover:bg-slate-800"
                        onClick={() => props.onToggle(option.id)}
                      >
                        <span>{option.label}</span>
                        <span className="text-[10px] uppercase tracking-[0.18em] text-slate-300">Remove</span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className="mt-5">
                <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-500">Available</p>
                <div className="mt-3 space-y-2">
                  {visibleOptions.length > 0 ? (
                    visibleOptions.map((option) => (
                      <button
                        key={option.id}
                        type="button"
                        className="flex w-full items-start justify-between gap-3 rounded-2xl border border-slate-200 bg-slate-50 px-3 py-3 text-left text-sm text-slate-700 transition hover:border-slate-300 hover:bg-slate-100"
                        onClick={() => {
                          props.onToggle(option.id);
                          setQuery("");
                        }}
                      >
                        <span>{option.label}</span>
                        <span className="text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-400">Add</span>
                      </button>
                    ))
                  ) : (
                    <p className="rounded-2xl border border-dashed border-slate-300 bg-slate-50 px-3 py-4 text-sm text-slate-500">
                      No matching items were found.
                    </p>
                  )}
                </div>
              </div>

              {hiddenCount > 0 ? (
                <p className="mt-3 text-xs text-slate-500">
                  {hiddenCount} more items are available. Use search to narrow the list.
                </p>
              ) : null}
            </div>

            <div className="border-t border-slate-200 px-5 py-4">
              <div className="flex items-center justify-between gap-3">
                <p className="text-sm text-slate-600">{summarizeAssignmentSelection(props.options, props.selected)}</p>
                <button
                  type="button"
                  className="rounded-2xl bg-slate-950 px-4 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
                  onClick={() => setIsOpen(false)}
                >
                  Done
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}

export function ClientAssignmentSummary(props: ClientAssignmentSummaryProps) {
  return (
    <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      <SummaryTile label="Groups" value={String(props.summary.fleetGroupCount)} />
      <SummaryTile label="Explicit nodes" value={String(props.summary.explicitNodeCount)} />
      <SummaryTile label="Covered nodes" value={String(props.summary.coveredNodeCount)} />
    </div>
  );
}

function SummaryTile(props: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white px-4 py-4">
      <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-500">{props.label}</p>
      <p className="mt-3 text-xl font-semibold tracking-tight text-slate-950">{props.value}</p>
    </div>
  );
}
