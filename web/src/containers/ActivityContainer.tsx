import { useState } from "react";
import { ActivityPage, Spinner } from "@panvex/ui";
import { useActivity } from "@/hooks/useActivity";

export function ActivityContainer() {
  const { jobs, auditEvents, isLoading } = useActivity();
  const [activeTab, setActiveTab] = useState("jobs");

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
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
