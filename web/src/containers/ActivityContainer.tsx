import { useState } from "react";
import { Spinner } from "@lost-coder/panvex-ui";
import { ActivityPage } from "@lost-coder/panvex-ui/pages";
import { useActivity } from "@/hooks/useActivity";
import { ErrorState } from "@/components/ErrorState";

export function ActivityContainer() {
  const { jobs, auditEvents, isLoading, error } = useActivity();
  const [activeTab, setActiveTab] = useState("jobs");

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  return (
    <ActivityPage
      jobs={jobs}
      auditEvents={auditEvents}
      activeTab={activeTab}
      onTabChange={setActiveTab}
    />
  );
}
