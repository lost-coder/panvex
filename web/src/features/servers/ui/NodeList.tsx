import { cn } from "@/ui/lib/cn";
import { NodeCard, type NodeCardProps } from "@/features/servers/ui/NodeCard";

export interface NodeListProps {
  nodes: NodeCardProps[];
  className?: string;
}

export function NodeList({ nodes, className }: Readonly<NodeListProps>) {
  return (
    <div className={cn("grid grid-cols-1 gap-2", "md:grid-cols-2", "xl:grid-cols-3", className)}>
      {nodes.map((node) => (
        <NodeCard key={node.name} {...node} />
      ))}
    </div>
  );
}
