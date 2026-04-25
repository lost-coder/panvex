// POSIX-safe single-quote escape: every char inside single quotes is
// taken literally except `'` itself, which closes the quote. Replace
// each `'` with `'\''` (close, escaped quote, reopen) so the result is
// always a single shell argv entry no matter what bytes the input
// contains.
//
// Always wrap, even for inputs that look "safe" — the cost is one byte
// either side and the operator gets a cleaner audit trail of what was
// substituted into the install command.
export function shellQuote(value: string): string {
  return "'" + value.replace(/'/g, "'\\''") + "'";
}

// Accepts what an operator might reasonably type for a node hostname:
// letters, digits, dot, dash, underscore. Capped at 64 chars (a typical
// hostname budget, generous for our use). Rejects whitespace, shell
// metachars, and quotes outright — the install command is meant to be
// pasted into a root shell, so anything that could break apart into a
// second token must not pass this filter.
const NODE_NAME_RE = /^[A-Za-z0-9._-]{1,64}$/;

export function isValidNodeName(value: string): boolean {
  return NODE_NAME_RE.test(value);
}
