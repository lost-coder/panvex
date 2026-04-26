import { createContext, useContext, useMemo } from "react";
import type { ReactNode } from "react";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

// Q5.U-Q-10: ServerDetailPage and its tab subtree share the same
// `server` payload and `serverId`. The previous version threaded both
// down through every tab as props, which made every subtree edit
// touch ServerDetailPage's render tree.  The context lets tabs read
// the active server directly so adding a new tab no longer requires
// editing every layer between it and the data.
//
// Keeping this in a sibling file (not on ServerDetailPage.tsx itself)
// lets the file export a single component, which the React refresh
// linter prefers.
export interface ServerDetailCtx {
  server: ServerDetailPageProps["server"];
  serverId: string;
}

const ctx = createContext<ServerDetailCtx | null>(null);

export function ServerDetailProvider({
  server,
  serverId,
  children,
}: {
  server: ServerDetailPageProps["server"];
  serverId: string;
  children: ReactNode;
}) {
  const value = useMemo(() => ({ server, serverId }), [server, serverId]);
  return <ctx.Provider value={value}>{children}</ctx.Provider>;
}

export function useServerDetailContext(): ServerDetailCtx {
  const v = useContext(ctx);
  if (!v) {
    throw new Error(
      "useServerDetailContext: must be rendered inside <ServerDetailProvider>",
    );
  }
  return v;
}
