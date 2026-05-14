// Phase-7 v2 — checklist restored, Metrics URL surfaced as a primary
// field (Telemt ships with metrics disabled by default, so we can't
// hide the knob inside Advanced), fleet group optional.
import { useTranslation } from "react-i18next";

import { StepIndicator } from "@/ui/primitives/StepIndicator";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { ConfigureStep } from "./steps/ConfigureStep";
import { InstallStep } from "./steps/InstallStep";
import { ConnectStep } from "./steps/ConnectStep";

// ─── Main ────────────────────────────────────────────────────────────

export function EnrollmentWizard(props: Readonly<EnrollmentWizardProps>) {
  const { t } = useTranslation("enrollment");
  const steps = [
    t("wizard.steps.configure"),
    t("wizard.steps.install"),
    t("wizard.steps.connect"),
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h3 className="text-title">{t("wizard.title")}</h3>
        <p className="text-sm text-fg-muted">
          {props.step === 1 && t("wizard.subtitle.step1")}
          {props.step === 2 && t("wizard.subtitle.step2")}
          {props.step === 3 && t("wizard.subtitle.step3")}
        </p>
      </div>

      <StepIndicator steps={steps} current={props.step - 1} />

      {props.step === 1 && <ConfigureStep {...props} />}
      {props.step === 2 && <InstallStep {...props} />}
      {props.step === 3 && <ConnectStep {...props} />}
    </div>
  );
}
