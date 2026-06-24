import { useTranslation } from "react-i18next";

import { Fold } from "../server-detail/components/Fold";
import { EnrollmentTimeline } from "./EnrollmentTimeline";
import { useEnrollmentLiveAttempt } from "./useEnrollmentLiveAttempt";

interface Props {
  /**
   * Once the bootstrap probe in AddServerContainer matches an agent,
   * pass its UUID here so the section can fetch the most recent
   * enrollment attempt for that agent and stream its timeline.
   */
  agentId: string | null;
}

// EnrollmentLiveSection is the AddServer wizard's live view of the
// in-flight enrollment timeline. It is intentionally a no-op until
// the wizard knows the agent ID — the bootstrap stage doesn't have it
// yet, and the backend's `enrollment_attempts.token_id` column isn't
// populated by the HTTP path, so token-based filtering would return
// nothing useful for inbound enrollment.
export function EnrollmentLiveSection({ agentId }: Readonly<Props>) {
  const { t } = useTranslation("enrollment");
  const { detail, isLoading } = useEnrollmentLiveAttempt(agentId);

  if (!agentId) return null;

  if (isLoading && !detail) {
    return (
      <Fold title={t("live.heading")}>
        <div className="text-sm text-fg-muted">{t("live.waiting")}</div>
      </Fold>
    );
  }

  if (!detail) {
    return (
      <Fold title={t("live.heading")}>
        <div className="text-sm text-fg-muted">{t("live.idle")}</div>
      </Fold>
    );
  }

  return (
    <Fold title={t("live.heading")}>
      <EnrollmentTimeline detail={detail} />
    </Fold>
  );
}
