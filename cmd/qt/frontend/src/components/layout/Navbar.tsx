import type { LucideIcon } from "lucide-react";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Globe2,
  LayoutDashboard,
  MessagesSquare,
  Pickaxe,
  ScrollText,
  Terminal,
  Map,
} from "lucide-react";
import { NavLink, useLocation } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";

type Page =
  | "overview"
  | "social"
  | "send"
  | "receive"
  | "transactions"
  | "network"
  | "mining"
  | "console"
  | "node-map";

type NavItem = {
  id: Page;
  label: string;
  enabled: boolean;
  to: string;
  icon: LucideIcon;
};

const primaryNav: NavItem[] = [
  { id: "overview", label: "Overview", enabled: true, to: "/", icon: LayoutDashboard },
  { id: "social", label: "Social", enabled: true, to: "/social", icon: MessagesSquare },
  { id: "node-map", label: "Node Map", enabled: true, to: "/node-map", icon: Map },
];

const upcomingNav: NavItem[] = [
  { id: "send", label: "Send", enabled: false, to: "/send", icon: ArrowUpFromLine },
  { id: "receive", label: "Receive", enabled: false, to: "/receive", icon: ArrowDownToLine },
  {
    id: "transactions",
    label: "Transactions",
    enabled: false,
    to: "/transactions",
    icon: ScrollText,
  },
  { id: "network", label: "Network", enabled: false, to: "/network", icon: Globe2 },
  { id: "mining", label: "Mining", enabled: false, to: "/mining", icon: Pickaxe },
  { id: "console", label: "Console", enabled: false, to: "/console", icon: Terminal },
];

function navActive(pathname: string, to: string): boolean {
  if (to === "/") return pathname === "/";
  return pathname === to || pathname.startsWith(`${to}/`);
}

function navButtonClass(active: boolean, enabled: boolean): string {
  return cn(
    "relative overflow-hidden rounded-lg border border-transparent bg-transparent text-[13px] font-medium tracking-tight",
    "mx-0.5 px-2.5 py-2 group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:min-h-8 group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:max-w-8 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 group-data-[collapsible=icon]:py-0",
    "transition-[color,background-color,border-color,box-shadow,transform] duration-200 ease-out",
    "outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-btc-gold)]/30 focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--color-btc-surface)]",
    enabled &&
      !active && [
        "text-[var(--color-btc-text-muted)]",
        "hover:!bg-[var(--color-overlay-hover)] hover:border-[var(--color-border-on-dark)] hover:text-[var(--color-btc-text)]",
        "active:!bg-transparent",
        "active:scale-[0.99] group-data-[collapsible=icon]:active:scale-95",
      ],
    enabled &&
      active &&
      cn(
        "border-[rgb(255_255_255_/0.09)] font-semibold text-[var(--color-btc-text)]",
        "shadow-[inset_2px_0_0_0_var(--color-btc-gold)]",
        "!bg-[rgb(255_255_255_/0.055)]",
        "hover:!bg-[rgb(255_255_255_/0.08)]",
        "group-data-[collapsible=icon]:shadow-none",
        "group-data-[collapsible=icon]:border-[var(--color-btc-border-light)] group-data-[collapsible=icon]:!bg-[rgb(255_255_255_/0.06)]",
        "group-data-[collapsible=icon]:hover:!bg-[rgb(255_255_255_/0.09)]",
        "active:scale-[0.99] group-data-[collapsible=icon]:active:scale-95",
      ),
    !enabled &&
      cn(
        "cursor-not-allowed border-transparent !bg-transparent text-[var(--color-btc-text-dim)] opacity-[0.42] hover:!bg-transparent active:!bg-transparent",
        "group-data-[collapsible=icon]:opacity-50",
      ),
  );
}

function NavIcon({
  Icon,
  active,
  enabled,
}: {
  Icon: LucideIcon;
  active: boolean;
  enabled: boolean;
}) {
  return (
    <Icon
      className={cn(
        "size-4 shrink-0 transition-[stroke-width,color,opacity] duration-200",
        enabled && !active && "opacity-90",
        enabled && active && "text-(--color-btc-gold)",
        !enabled && "opacity-70",
      )}
      strokeWidth={active && enabled ? 2.25 : 1.65}
      aria-hidden
    />
  );
}

export function Navbar() {
  const coinInfo = useCoinInfo();
  const { pathname } = useLocation();
  const brandInitial = coinInfo.name.trim().charAt(0).toUpperCase() || "—";

  return (
    <Sidebar
      collapsible="icon"
      variant="sidebar"
      className="border-r"
      style={{ borderColor: "var(--color-btc-border)" }}
    >
      <SidebarHeader className="relative z-10 gap-0 border-0 px-2.5 pb-2.5 pt-3 group-data-[collapsible=icon]:px-0 group-data-[collapsible=icon]:pb-1.5 group-data-[collapsible=icon]:pt-2">
        <SidebarMenu className="group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:gap-0">
          <SidebarMenuItem className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0">
            <SidebarMenuButton
              size="lg"
              asChild
              tooltip={coinInfo.name}
              className={cn(
                "h-auto min-h-11 gap-2.5 rounded-lg border border-transparent bg-transparent px-2 py-2 transition-[border-color] duration-200",
                "hover:bg-(--color-overlay-hover)! hover:border-(--color-border-on-dark-soft)",
                "focus-visible:ring-2 focus-visible:ring-(--color-btc-gold)/30",
                "group-data-[collapsible=icon]:size-8! group-data-[collapsible=icon]:min-h-8! group-data-[collapsible=icon]:p-0! group-data-[collapsible=icon]:justify-center",
              )}
            >
              <NavLink
                to="/"
                end
                aria-label={`${coinInfo.name} home`}
                className={cn(
                  "flex min-w-0 items-center gap-2.5 text-left",
                  "group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:size-full group-data-[collapsible=icon]:max-w-8 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:px-0",
                )}
              >
                <span
                  className={cn(
                    "flex size-8 shrink-0 select-none items-center justify-center text-[15px] font-semibold tracking-tight",
                    "text-(--color-btc-gold) antialiased",
                    "group-data-[collapsible=icon]:p-0 group-data-[collapsible=icon]:text-[16px] group-data-[collapsible=icon]:leading-none",
                  )}
                >
                  {brandInitial}
                </span>
                <div className="grid min-w-0 flex-1 leading-tight group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:w-0 group-data-[collapsible=icon]:min-w-0 group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:overflow-hidden group-data-[collapsible=icon]:p-0">
                  <span
                    className="truncate text-[14px] font-semibold tracking-tight"
                    style={{ color: "var(--color-btc-text)" }}
                  >
                    {coinInfo.name}
                  </span>
                  <span
                    className="truncate text-[11px] font-normal tabular-nums"
                    style={{ color: "var(--color-btc-text-dim)" }}
                  >
                    v{coinInfo.version}
                  </span>
                </div>
              </NavLink>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div
          className="mx-0 mt-2.5 h-px group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:mt-0"
          style={{
            background: "linear-gradient(90deg, rgb(247 147 26 / 0.28) 0%, transparent 72%)",
          }}
        />
      </SidebarHeader>

      <SidebarContent className="relative z-10 gap-4 px-1.5 pb-2 group-data-[collapsible=icon]:gap-2 group-data-[collapsible=icon]:px-0">
        <SidebarGroup className="p-0 group-data-[collapsible=icon]:items-center">
          <SidebarGroupLabel
            className="mb-1.5 px-2.5 text-[10px] font-semibold uppercase tracking-[0.14em]"
            style={{ color: "var(--color-btc-text-dim)" }}
          >
            Navigation
          </SidebarGroupLabel>
          <SidebarGroupContent className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:items-center">
            <SidebarMenu className="gap-0.5 group-data-[collapsible=icon]:items-center">
              {primaryNav.map((item) => {
                const active = navActive(pathname, item.to);
                return (
                  <SidebarMenuItem
                    key={item.id}
                    className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:w-full group-data-[collapsible=icon]:justify-center"
                  >
                    <SidebarMenuButton
                      asChild
                      isActive={active}
                      tooltip={item.label}
                      className={navButtonClass(active, item.enabled)}
                    >
                      <NavLink
                        to={item.to}
                        end={item.to === "/"}
                        className="flex min-w-0 w-full items-center gap-2 group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:w-8 group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:max-w-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0"
                      >
                        <NavIcon Icon={item.icon} active={active} enabled={item.enabled} />
                        <span className="group-data-[collapsible=icon]:hidden">{item.label}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="p-0 group-data-[collapsible=icon]:items-center">
          <SidebarGroupLabel
            className="mb-1.5 px-2.5 text-[10px] font-semibold uppercase tracking-[0.14em]"
            style={{ color: "var(--color-btc-text-dim)" }}
          >
            Coming soon
          </SidebarGroupLabel>
          <SidebarGroupContent className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:items-center">
            <SidebarMenu className="gap-0.5 group-data-[collapsible=icon]:items-center">
              {upcomingNav.map((item) => (
                <SidebarMenuItem
                  key={item.id}
                  className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:w-full group-data-[collapsible=icon]:justify-center"
                >
                  <SidebarMenuButton
                    disabled
                    tooltip={`${item.label} — coming soon`}
                    className={navButtonClass(false, false)}
                  >
                    <NavIcon Icon={item.icon} active={false} enabled={false} />
                    <span className="group-data-[collapsible=icon]:hidden">{item.label}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter
        className="relative z-10 mt-auto border-t px-3 py-3 group-data-[collapsible=icon]:px-2 group-data-[collapsible=icon]:py-2"
        style={{ borderColor: "var(--color-btc-border)" }}
      >
        <p
          className="text-[10px] font-medium leading-relaxed tracking-wide group-data-[collapsible=icon]:hidden"
          style={{ color: "var(--color-btc-text-dim)" }}
        >
          {coinInfo.copyright}
        </p>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
