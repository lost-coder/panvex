import { ReactNode } from "react";
import { MobileNav } from "./mobile-nav";
import { Rail } from "./rail";
import { TopBar } from "./top-bar";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div
      className="grid min-h-screen
        grid-rows-[var(--topbar-h)_1fr] grid-cols-[1fr]
        md:grid-cols-[var(--rail-w)_1fr] md:grid-rows-[var(--topbar-h)_1fr]"
      style={{
        gridTemplateAreas: `
          'topbar'
          'main'
        `,
      }}
    >
      <Rail className="hidden md:flex" />
      <TopBar />
      <main
        className="overflow-y-auto p-5 pb-10 bg-page"
        style={{ gridArea: "main", scrollbarWidth: "thin", scrollbarColor: "var(--border) transparent" }}
      >
        {children}
      </main>
      <MobileNav className="md:hidden" />
    </div>
  );
}
