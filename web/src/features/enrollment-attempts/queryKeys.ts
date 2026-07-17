// BP-02: feature-local React-Query key factory for enrollment-attempts.
// Before P3-3.3 the keys were inline literals in three files; the factory
// is needed by the enrollment.* branch in shared/events/event-invalidations.ts.
export const enrollmentAttemptsKeys = {
  all: ["enrollment-attempts"] as const,
  page: (filter: unknown) => [...enrollmentAttemptsKeys.all, "page", filter] as const,
  detail: (id: string) => [...enrollmentAttemptsKeys.all, "detail", id] as const,
  byAgent: (agentId: string) => [...enrollmentAttemptsKeys.all, "by-agent", agentId] as const,
};
