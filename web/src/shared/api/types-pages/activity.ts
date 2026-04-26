// --- Activity (Jobs + Audit) ---

export interface JobListItem {
  id: string;
  action: string;
  status: string;
  actorId: string;
  /** Resolved human label for actorId (username). Falls back to shortId. */
  actorLabel?: string | undefined;
  targetCount: number;
  createdAtUnix: number;
  /** First failing target's result_text, if any. Shown under the action row. */
  failureReason?: string | undefined;
}

export interface AuditListItem {
  id: string;
  actorId: string;
  /** Resolved human label for actorId (username). */
  actorLabel?: string | undefined;
  action: string;
  targetId: string;
  /** Resolved human label for targetId (client name, node_name, or username). */
  targetLabel?: string | undefined;
  /** Entity kind derived from action namespace ("user", "client", "agent", …). */
  targetKind?: string | undefined;
  createdAtUnix: number;
}

export interface ActivityPageProps {
  jobs: JobListItem[];
  auditEvents: AuditListItem[];
  activeTab: string;
  onTabChange: (tab: string) => void;
  /** Non-fatal warning when actor/target label lookup failed — rows render
   *  with raw UUIDs. Rendered as a banner above the list. */
  lookupError?: string | null;
}
