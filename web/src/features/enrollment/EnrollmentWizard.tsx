// Phase-7 v2 — checklist restored, Metrics URL surfaced as a primary
// field (Telemt ships with metrics disabled by default, so we can't
// hide the knob inside Advanced), fleet group optional.
import { StepIndicator } from "@/ui/primitives/StepIndicator";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { ConfigureStep } from "./steps/ConfigureStep";
import { InstallStep } from "./steps/InstallStep";
import { ConnectStep } from "./steps/ConnectStep";

const STEPS = ["Configure", "Install", "Connect"];

// ─── Main ────────────────────────────────────────────────────────────

export function EnrollmentWizard(props: Readonly<EnrollmentWizardProps>) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h3 className="text-title">Add server node</h3>
        <p className="text-sm text-fg-muted">
          {props.step === 1 && "Pick a node name; we'll mint a one-shot token."}
          {props.step === 2 && "Run this command on the target Linux server as root."}
          {props.step === 3 && "Waiting for the agent to come online."}
        </p>
      </div>

      <StepIndicator steps={STEPS} current={props.step - 1} />

      {props.step === 1 && <ConfigureStep {...props} />}
      {props.step === 2 && <InstallStep {...props} />}
      {props.step === 3 && <ConnectStep {...props} />}
    </div>
  );
}
