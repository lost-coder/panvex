// BP-02: feature-local React-Query key factory для enrollment-attempts.
// До P3-3.3 ключи были инлайновыми литералами в трёх файлах; фабрика
// нужна ветке enrollment.* в shared/events/event-invalidations.ts.
export const enrollmentAttemptsKeys = {
  all: ["enrollment-attempts"] as const,
  page: (filter: unknown) => [...enrollmentAttemptsKeys.all, "page", filter] as const,
  detail: (id: string) => [...enrollmentAttemptsKeys.all, "detail", id] as const,
  byAgent: (agentId: string) => [...enrollmentAttemptsKeys.all, "by-agent", agentId] as const,
};
