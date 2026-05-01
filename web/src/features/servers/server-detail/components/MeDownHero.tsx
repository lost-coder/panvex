import type { ServerEvent } from "../format";

// Hero banner shown when an agent is in `me_down` state — middle proxy
// is required, ME runtime is not ready, and ME→DC fallback is disabled.
// Surfaces the recent events feed inline so operators have a starting
// point for triage without bouncing into the Events tab.
export function MeDownHero({ recentEvents }: { recentEvents: ServerEvent[] }) {
  return (
    <section className="rounded-md border border-status-error bg-status-error/10 p-6 flex flex-col gap-3">
      <h2 className="text-status-error text-lg font-semibold">
        ME pool unavailable, traffic stopped
      </h2>
      <p className="text-fg text-sm">
        Telemt is configured to require Middle-End proxies. The pool failed to
        initialise and ME→Direct fallback is disabled. New client connections
        are not being accepted.
      </p>
      {recentEvents.length > 0 && (
        <ul className="text-xs font-mono text-fg-muted">
          {recentEvents.slice(0, 5).map((e) => (
            <li key={e.seq}>
              {new Date(e.tsEpochSecs * 1000).toISOString()} — {e.eventType}: {e.context}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
