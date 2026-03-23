import { ReactNode } from "react";
import { MobileNav } from "./mobile-nav";
import { Rail } from "./rail";
import { TopBar } from "./top-bar";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-page">
      <Rail className="hidden md:flex" />
      <div className="flex flex-col min-h-screen md:pl-[var(--rail-w)]">
        <TopBar />
        <main
          className="flex-1 overflow-y-auto p-5 pb-20 md:pb-5"
          style={{ scrollbarWidth: "thin", scrollbarColor: "var(--border) transparent" }}
        >
          {children}
        </main>
      </div>
      <MobileNav className="md:hidden" />
    </div>
  );
}
