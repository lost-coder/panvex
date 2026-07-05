import type { Status } from "@/ui/tokens/colors";
import type { FleetGroupOption } from "./common";

// --- Client Form ---

export interface NodeOption {
  id: string;
  name: string;
  status: Status;
  fleetGroup: string;
}

export interface ClientFormData {
  name: string;
  userAdTag: string;
  /**
   * Controls the user_ad_tag_auto flag sent to the backend. When
   * true (default for create mode), the control-plane mints a tag
   * automatically if `userAdTag` is empty. When false, the raw
   * `userAdTag` is stored as-is (empty means the client has no tag).
   */
  userAdTagAuto: boolean;
  expirationRfc3339: string;
  maxTcpConns: number;
  maxUniqueIps: number;
  dataQuotaBytes: number;
  fleetGroupIds: string[];
  agentIds: string[];
}

export interface ClientAgentOption {
  id: string;
  nodeName: string;
  fleetGroupId: string;
  online?: boolean | undefined;
}

export interface ClientFormSheetProps {
  mode: "create" | "edit";
  data: ClientFormData;
  onChange: (data: ClientFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
  /** Fleet groups available for assignment. Omitted → selector hidden. */
  fleetGroups?: FleetGroupOption[] | undefined;
  /** Agents available for explicit assignment. Omitted → selector hidden. */
  agents?: ClientAgentOption[] | undefined;
}

export interface SecretRevealProps {
  secret: string;
  onDismiss: () => void;
}
