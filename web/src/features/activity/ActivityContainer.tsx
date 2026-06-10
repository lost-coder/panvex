import { useState } from "react";
import { ActivityPage } from "./ActivityPage";
import { useActivity } from "./hooks/useActivity";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";

export function ActivityContainer() {
  const { jobs, auditEvents, isLoading, error, lookupError, refetch } = useActivity();
  const [activeTab, setActiveTab] = useState("jobs");

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
      activeTab={activeTab}
      onTabChange={setActiveTab}
      lookupError={lookupError}
    />
  );
}
