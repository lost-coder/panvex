// Pure helpers for the `restart_spec` string TelemtUpdateStrategy carries
// ("systemd:<unit>" / "procd:<service>" / "openrc:<service>" /
// "runit:<service>" / "command:<argv>"). Mirrors the parsing rules of
// internal/restartspec/restartspec.go (Task 1) so the form composes the
// exact same shape the server-side parser accepts — kept intentionally
// dumb (string in, string out) since the server is the source of truth
// for validation; this only drives the UI's preset/name split.

export type RestartPreset = "systemd" | "procd" | "openrc" | "runit" | "custom";

const NAMED_PRESETS: readonly Exclude<RestartPreset, "custom">[] = [
  "systemd",
  "procd",
  "openrc",
  "runit",
];

export interface RestartSpecForm {
  preset: RestartPreset;
  /** Unit/service name — meaningful only for the named presets. */
  name: string;
  /** Full command line — meaningful only for preset "custom". */
  command: string;
}

export const emptyRestartSpecForm: RestartSpecForm = {
  preset: "systemd",
  name: "",
  command: "",
};

/**
 * Splits a "<kind>:<rest>" spec into a preset + name/command. Anything that
 * doesn't parse as a recognised kind (empty, no ':', or an unknown kind)
 * falls back to the "custom" preset with the raw spec as the command, so
 * round-tripping an unrecognised value never silently drops it.
 */
export function parseRestartSpec(spec: string): RestartSpecForm {
  const trimmed = spec.trim();
  if (!trimmed) return { ...emptyRestartSpecForm, preset: "systemd" };

  const sep = trimmed.indexOf(":");
  if (sep === -1) return { preset: "custom", name: "", command: trimmed };

  const kind = trimmed.slice(0, sep);
  const rest = trimmed.slice(sep + 1).trim();

  if (kind === "command") return { preset: "custom", name: "", command: rest };
  if ((NAMED_PRESETS as readonly string[]).includes(kind)) {
    return { preset: kind as RestartPreset, name: rest, command: "" };
  }
  return { preset: "custom", name: "", command: trimmed };
}

/** Inverse of parseRestartSpec — empty name/command composes to "". */
export function composeRestartSpec(form: RestartSpecForm): string {
  if (form.preset === "custom") {
    const command = form.command.trim();
    return command ? `command:${command}` : "";
  }
  const name = form.name.trim();
  return name ? `${form.preset}:${name}` : "";
}

/**
 * Client-side mirror of restartspec.go's validateName: rejects names that
 * would let an operator-supplied string smuggle in extra arguments or
 * escape a fixed directory. Only applies to the named presets — "custom"
 * is validated as a non-empty command instead.
 */
export function isValidRestartName(name: string): boolean {
  const trimmed = name.trim();
  if (!trimmed) return false;
  if (/\s/.test(trimmed)) return false;
  if (trimmed.includes("/")) return false;
  if (trimmed.includes("..")) return false;
  return true;
}
