import { useState } from "react";
import { ActivityPage } from "./ActivityPage";
import { useActivity } from "./hooks/useActivity";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";

export function ActivityContainer() {
  const { jobs, auditEvents, isLoading, error, lookupError, refetch } = useActivity();
  // U-17: don't hard-default to Jobs — landing on an empty Jobs tab while
  // the Audit trail holds hundreds of entries is a dead first screen. Until
  // the operator explicitly picks a tab, show whichever has content (Jobs
  // when something is in flight, otherwise the always-populated Audit log).
  const [activeTab, setActiveTab] = useState<string | null>(null);
  const effectiveTab = activeTab ?? (jobs.length > 0 ? "jobs" : "audit");

  if (isLoading) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={8} />
      </div>
    );
  }

  if (error) {
    return <ErrorState description={error.message} onRetry={() => void refetch()} />;
  }

  return (
    <ActivityPage
      jobs={jobs}
      auditEvents={auditEvents}
      activeTab={effectiveTab}
      onTabChange={setActiveTab}
      lookupError={lookupError}
    />
  );
}
