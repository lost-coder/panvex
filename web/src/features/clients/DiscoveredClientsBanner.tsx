import { ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";

export interface DiscoveredClientsBannerProps {
  count: number;
  onClick?: (() => void) | undefined;
}

export function DiscoveredClientsBanner({ count, onClick }: Readonly<DiscoveredClientsBannerProps>) {
  const { t } = useTranslation("clients");
  if (count <= 0) return null;

  return (
    <button
      type="button"
      onClick={onClick}
      className="w-full flex items-center gap-3 rounded-lg border border-status-warn/30 bg-status-warn/10 px-4 py-3 text-sm font-medium text-status-warn hover:bg-status-warn/15 transition-colors cursor-pointer"
    >
      <span className="inline-flex items-center justify-center rounded-full bg-status-warn text-white text-xs font-bold min-w-[24px] h-6 px-1.5">
        {count}
      </span>
      <span>{t("banner.pending", { count })}</span>
      <ChevronRight className="ml-auto w-4 h-4 text-status-warn/60" />
    </button>
  );
}
