#!/usr/bin/env node
// Fails when an installed package violates a peer range declared by one of its
// dependents — npm calls this an "invalid" node.
//
// Why this exists: the 2026-08-07 Dependabot sweep twice produced a tree where
// a scoped package moved without the root package it pins a peer range on
// (@storybook/* -> storybook, size-limit -> @size-limit/file). Nothing in CI
// noticed: builds, tests, lint, size budgets and npm audit were all green,
// because npm installs the skewed tree anyway and only reports the mismatch on
// an explicit `npm ls <name>`. Both were caught by a human reading the diff.
//
// Note on the obvious alternatives:
//   npm ls           exits 0 — top-level only, does not descend to the mismatch
//   npm ls --all     exits 1 on a HEALTHY tree — optional platform packages
//                    (@emnapi/*, @napi-rs/*) show up as "extraneous"
// Hence: read the JSON and key on `invalid` alone, ignoring `extraneous`.
//
// ALLOWLIST is a ratchet, same convention as internal/controlplane/archguard:
// entries may be removed, never added without a written reason.

import { execFileSync } from 'node:child_process'

const ALLOWLIST = [
  {
    package: 'eslint',
    requiredBy: 'eslint-plugin-jsx-a11y',
    reason:
      'jsx-a11y 6.10.2 (latest as of 2026-08-07) still caps its peer range at ' +
      'eslint ^9 while the repo runs eslint 10. Upstream has published nothing ' +
      'newer; the plugin works. Drop this entry once jsx-a11y ships eslint 10 support.',
  },
]

// npm exits non-zero whenever the tree has ANY problem, extraneous included —
// which is the normal state here. The JSON on stdout is still complete and is
// what we actually judge, so a non-zero exit must not abort the check.
let raw
try {
  raw = execFileSync('npm', ['ls', '--all', '--json'], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'ignore'],
  })
} catch (err) {
  raw = err.stdout
  if (!raw) {
    console.error('npm ls --all --json produced no output:', err.message)
    process.exit(2)
  }
}

const findings = new Map()

function walk(node) {
  for (const [name, dep] of Object.entries(node.dependencies ?? {})) {
    if (typeof dep !== 'object' || dep === null) continue
    if (dep.invalid) {
      // dep.invalid reads: `"<range>" from node_modules/<pkg>[, "<range>" from
      // node_modules/<pkg>]...`. The same mismatch surfaces once per path
      // through the tree, so key on the package alone and union the requirers.
      const requirers = [...String(dep.invalid).matchAll(/from node_modules\/(\S+?)(?:,|$)/g)].map(
        (m) => m[1],
      )
      const key = `${name}@${dep.version}`
      const existing = findings.get(key)
      findings.set(key, {
        name,
        version: dep.version,
        requiredBy: [...new Set([...(existing?.requiredBy ?? []), ...requirers])],
        range: String(dep.invalid).match(/^"([^"]+)"/)?.[1] ?? String(dep.invalid),
      })
    }
    walk(dep)
  }
}

walk(JSON.parse(raw))

// A finding is allowed only if EVERY package demanding the unmet range is
// allowlisted — one unexpected requirer is enough to fail.
const unexpected = [...findings.values()].filter((f) =>
  f.requiredBy.some((by) => !ALLOWLIST.some((a) => a.package === f.name && a.requiredBy === by)),
)

if (unexpected.length > 0) {
  console.error('Dependency tree has unsatisfied peer ranges:\n')
  for (const f of unexpected) {
    console.error(`  ${f.name}@${f.version} does not satisfy "${f.range}"`)
    console.error(`    required by: ${f.requiredBy.join(', ')}`)
  }
  console.error(
    '\nA package moved without the peer it is pinned against. Bump them together,' +
      '\nor add an entry to ALLOWLIST in web/scripts/check-dep-tree.mjs with a reason.',
  )
  process.exit(1)
}

console.log(`Dependency tree OK (${ALLOWLIST.length} allowlisted exception(s)).`)
