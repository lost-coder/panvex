/**
 * R1.2 (audit 2026-07-07 §1.2): await the mutation and close the sheet
 * ONLY when it resolved. The previous inline pattern
 * `await onCreate(x); if (!createError) setOpen(false)` read a prop
 * captured before the submit — after a failed attempt the stale error
 * kept the sheet open even when the retry succeeded.
 *
 * `action` must reject on failure (TanStack Query's mutateAsync does).
 * The rejection is swallowed here because the mutation error already
 * surfaces through the container's `error` prop on the sheet.
 *
 * The return type is `() => unknown` rather than `() => Promise<unknown>`
 * because container callbacks are typed `void | Promise<void>`; `await`
 * handles both a promise and a bare value transparently.
 */
export async function submitAndClose(
  action: () => unknown,
  close: () => void,
): Promise<void> {
  try {
    await action();
  } catch {
    return;
  }
  close();
}
