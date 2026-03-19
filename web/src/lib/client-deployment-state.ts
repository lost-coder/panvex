import type { ClientDeployment } from "./api";

type ClientDeploymentAlertTone = "warning" | "danger";

export type ClientDeploymentAlert = {
  tone: ClientDeploymentAlertTone;
  title: string;
  description: string;
};

export function shouldPollClientDetail(deployments: ClientDeployment[]): boolean {
  return deployments.some((deployment) => deployment.status === "queued" || deployment.status === "pending");
}

export function buildClientDeploymentAlert(deployments: ClientDeployment[]): ClientDeploymentAlert | null {
  const failedDeployment = deployments.find((deployment) => deployment.status === "failed");
  if (failedDeployment) {
    return {
      tone: "danger",
      title: "Client rollout failed",
      description: failedDeployment.last_error
        ? `${failedDeployment.agent_id}: ${failedDeployment.last_error}`
        : `${failedDeployment.agent_id}: Panvex did not receive a detailed Telemt error.`
    };
  }

  if (shouldPollClientDetail(deployments)) {
    return {
      tone: "warning",
      title: "Client rollout in progress",
      description: "Panvex is waiting for assigned nodes to apply the latest client state."
    };
  }

  return null;
}
