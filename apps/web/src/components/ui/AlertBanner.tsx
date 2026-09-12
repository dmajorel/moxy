import type { ReactNode } from "react";
import { IconAlertTriangle, IconCheck, IconRefresh } from "@tabler/icons-react";

/**
 * The banner that closes a cluster card: quorum, memory pressure, pending
 * update. It formats nothing — the sentence comes in as `children`.
 */
export type AlertBannerVariant = "warning" | "neutral";

/** The three icons the mocks use, named so a formatter can pick one by value. */
export type AlertBannerIcon = "alert" | "check" | "refresh";

export interface AlertBannerProps {
  /** Defaults to "neutral": amber is reserved for something to act on. */
  variant?: AlertBannerVariant;
  /** Defaults to "alert" on warning, "check" on neutral. */
  icon?: AlertBannerIcon;
  className?: string;
  children?: ReactNode;
}

const VARIANT_CLASSES: Record<AlertBannerVariant, string> = {
  warning: "bg-bg-warning text-text-warning",
  neutral: "bg-surface-1 text-text-secondary",
};

const DEFAULT_ICON: Record<AlertBannerVariant, AlertBannerIcon> = {
  warning: "alert",
  neutral: "check",
};

const ICONS = {
  alert: IconAlertTriangle,
  check: IconCheck,
  refresh: IconRefresh,
};

const BASE_CLASSES =
  "flex items-center gap-2 rounded-card px-2.5 py-2 text-[12px]";

export function AlertBanner({
  variant = "neutral",
  icon,
  className,
  children,
}: AlertBannerProps) {
  const Icon = ICONS[icon ?? DEFAULT_ICON[variant]];
  const classes = [BASE_CLASSES, VARIANT_CLASSES[variant], className]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={classes}>
      {/* The sentence next to it carries the meaning; the icon is decoration. */}
      <Icon className="shrink-0" size={14} stroke={1.75} aria-hidden />
      <span>{children}</span>
    </div>
  );
}
