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
export function EnrollmentLiveSection({ agentId }: Props) {
  const { detail, isLoading } = useEnrollmentLiveAttempt(agentId);

  if (!agentId) return null;

  if (isLoading && !detail) {
    return (
      <section className="mt-6">
        <h3 className="text-base font-medium mb-3">Подключение агента</h3>
        <div className="text-sm text-fg-muted">Ожидаем подключения агента…</div>
      </section>
    );
  }

  if (!detail) {
    return (
      <section className="mt-6">
        <h3 className="text-base font-medium mb-3">Подключение агента</h3>
        <div className="text-sm text-fg-muted">
          Подключение ещё не начиналось.
        </div>
      </section>
    );
  }

  return (
    <section className="mt-6">
      <h3 className="text-base font-medium mb-3">Подключение агента</h3>
      <EnrollmentTimeline detail={detail} />
    </section>
  );
}
