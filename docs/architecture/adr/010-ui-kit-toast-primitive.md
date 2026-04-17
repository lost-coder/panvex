# ADR-010: UI-kit Toast exported as primitive, web wires provider

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-FE-03

## Context

The Phase 2 work introduced several flows that needed transient
feedback — session-expired notices (ADR-008), save confirmations,
retention warnings, copy-to-clipboard acknowledgements — but the
dashboard had no shared toast mechanism. Each feature was on track
to invent its own. Meanwhile, `@lost-coder/panvex-ui` has a clear
charter: it owns *visual primitives* and must not know anything
about panvex internals (see `/home/killer/Projects/Panvex/CLAUDE.md`).
That constraint shaped the split: the kit should provide the
rendering surface, the web app should own the subscription and
routing of actual toasts.

## Decision

Two-sided split:

- **panvex-ui** exports a `Toast` primitive (visual element) and a
  `ToastViewport` container that renders a stack of toasts with
  enter/exit animations, variants, and dismissal. These components
  are pure — no context, no state machine, no panvex-specific
  semantics.
- **panvex/web** owns a `ToastProvider` that holds the toast queue,
  exposes a `useToast()` hook (`toast.success`, `toast.error`,
  `toast.info`, `toast.custom`), and renders the UI-kit
  `ToastViewport` with the current queue. Dismissal timing,
  deduplication, and integration with `panvex:session-expired` live
  here.

## Alternatives considered

- **Web owns everything.** Rejected: the visual treatment (colors,
  shadows, motion, dark-mode handling) is exactly what the UI kit
  exists to standardise. Duplicating it in the app would drift out
  of sync with the kit's other components.
- **UI kit owns everything, including the hook.** Rejected for the
  same reason the CLAUDE.md rule exists: the kit would start
  acquiring product-specific concerns (session events, analytics,
  i18n keys) and become tightly coupled to panvex. Once that door
  is open it cannot be closed.
- **Radix UI's built-in Toast.** Considered and would have worked,
  but the kit already has a design language for overlays, and
  adding Radix as a dependency for one primitive felt heavy. If
  we later adopt Radix for other surfaces we can revisit and swap
  the internals without changing the public API.

## Consequences

- Adding a new toast in the app is a one-liner:
  `const toast = useToast(); toast.success("Saved")`.
- The UI kit's `Toast` primitive is a stable public export. Any
  breaking change to its props is a semver-breaking change to the
  kit, so the props surface is kept intentionally narrow
  (`variant`, `title`, `description`, `action`, `onDismiss`).
- The `ToastProvider` must be mounted near the root of the React
  tree, above the router, so it survives route changes. This is
  documented in the web app's `main.tsx`.
