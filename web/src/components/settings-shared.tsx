import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

export function SettingsSection(props: { eyebrow: string; title: string; description: string; children: ReactNode }) {
  return (
    <section className="app-card rounded-[32px]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--app-text-tertiary)]">{props.eyebrow}</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-[var(--app-text-primary)]">{props.title}</h3>
      <p className="mt-3 text-sm leading-6 text-[var(--app-text-secondary)]">{props.description}</p>
      <div className="mt-6">{props.children}</div>
    </section>
  );
}

export function Field(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
  placeholder?: string;
  helperText?: string;
  disabled?: boolean;
}) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-[var(--app-text-secondary)]">{props.label}</span>
      <input
        type={props.type ?? "text"}
        className="app-control rounded-2xl text-sm"
        value={props.value}
        placeholder={props.placeholder}
        disabled={props.disabled}
        onChange={(event) => props.onChange(event.target.value)}
      />
      {props.helperText ? <p className="mt-2 text-xs leading-5 text-[var(--app-text-tertiary)]">{props.helperText}</p> : null}
    </label>
  );
}

export function SelectField(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
  helperText?: string;
  disabled?: boolean;
}) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-[var(--app-text-secondary)]">{props.label}</span>
      <select
        className="app-control rounded-2xl text-sm"
        value={props.value}
        disabled={props.disabled}
        onChange={(event) => props.onChange(event.target.value)}
      >
        {props.options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {props.helperText ? <p className="mt-2 text-xs leading-5 text-[var(--app-text-tertiary)]">{props.helperText}</p> : null}
    </label>
  );
}

export function ErrorText(props: { message: string }) {
  return <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">{props.message}</p>;
}

export function StatusBadge(props: { enabled: boolean }) {
  return (
    <span
      className={`rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] ${
        props.enabled ? "bg-emerald-100 text-emerald-900" : "bg-slate-200 text-slate-700"
      }`}
    >
      {props.enabled ? "Enabled" : "Disabled"}
    </span>
  );
}

export function CopyBlock(props: { label: string; value: string }) {
  return (
    <div className="app-card-muted rounded-3xl p-4">
      <div className="flex items-center justify-between gap-4">
        <div className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">{props.label}</div>
        <button
          type="button"
          className="app-button-secondary rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em]"
          onClick={() => void navigator.clipboard.writeText(props.value)}
        >
          Copy
        </button>
      </div>
      <pre className="mt-4 overflow-x-auto whitespace-pre-wrap break-all text-sm text-[var(--app-text-primary)]">{props.value}</pre>
    </div>
  );
}

export function SettingsState(props: { title: string; description: string }) {
  return (
    <div className="app-card rounded-[32px] p-8">
      <h3 className="text-2xl font-semibold tracking-tight text-[var(--app-text-primary)]">{props.title}</h3>
      <p className="mt-3 text-sm text-[var(--app-text-secondary)]">{props.description}</p>
    </div>
  );
}

export function AccordionSection(props: {
  title: string;
  description?: string;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
  trailing?: ReactNode;
}) {
  return (
    <div className="app-card-muted rounded-3xl">
      <button
        type="button"
        className="flex w-full items-start justify-between gap-4 px-5 py-4 text-left"
        onClick={props.onToggle}
      >
        <div className="min-w-0">
          <div className="text-base font-semibold text-[var(--app-text-primary)]">{props.title}</div>
          {props.description ? <p className="mt-1 text-sm leading-6 text-[var(--app-text-secondary)]">{props.description}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-3">
          {props.trailing}
          <span className="app-button-secondary rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em]">
            {props.open ? "Hide" : "Open"}
          </span>
        </div>
      </button>
      {props.open ? <div className="border-t border-[var(--app-border)] px-5 py-5">{props.children}</div> : null}
    </div>
  );
}

export function ModalFrame(props: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="app-overlay fixed inset-0 z-40 backdrop-blur-sm" />
        <Dialog.Content className="app-card fixed left-1/2 top-1/2 z-50 w-[min(92vw,34rem)] -translate-x-1/2 -translate-y-1/2 rounded-[32px]">
          <div className="flex items-start justify-between gap-4">
            <div>
              <Dialog.Title className="text-2xl font-semibold tracking-tight text-[var(--app-text-primary)]">{props.title}</Dialog.Title>
              <Dialog.Description className="mt-2 text-sm leading-6 text-[var(--app-text-secondary)]">{props.description}</Dialog.Description>
            </div>
            <Dialog.Close className="app-button-secondary rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em]">
              Close
            </Dialog.Close>
          </div>
          <div className="mt-6">{props.children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
