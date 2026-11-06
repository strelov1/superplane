import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { ChartLine, Cpu, Home, Workflow } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Link, useLocation } from "react-router";
import { automationsPath, factoryAgentPath, factoryHomePath, factoryVelocityPath } from "../lib/factoryPagePaths";
import {
  factoriesRailControlClassName,
  isAgentPath,
  isAutomationsPath,
  isBoardPath,
  isVelocityPath,
} from "./factoriesRail";

interface FactoriesSidebarNavProps {
  organizationId: string;
  factoryKey: string;
  lineId?: string;
  /** Public board keeps the home icon and hides Agent. */
  showAgent?: boolean;
  /** Public board keeps the home icon and hides Velocity. */
  showVelocity?: boolean;
}

function railLinkClassName(isCurrent: boolean) {
  return cn(factoriesRailControlClassName, isCurrent && "bg-sidebar-accent text-foreground");
}

function RailNavLink({
  to,
  label,
  Icon,
  testId,
  isCurrent,
}: {
  to: string;
  label: string;
  Icon: LucideIcon;
  testId: string;
  isCurrent: boolean;
}) {
  return (
    <Tooltip delayDuration={350}>
      <TooltipTrigger asChild>
        <Link
          to={to}
          aria-label={label}
          aria-current={isCurrent ? "page" : undefined}
          data-testid={testId}
          className={railLinkClassName(isCurrent)}
        >
          <Icon className="size-3.5" aria-hidden />
        </Link>
      </TooltipTrigger>
      <TooltipContent side="right" sideOffset={6}>
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Icon rail under the workspace switcher: the line board, Agent, Automations, and Velocity.
 * Automations is the only way in to automations not attached to a board column
 * (schedule or webhook triggered); they have no entry on the board.
 * Workspace settings open from the workspace switcher.
 */
export function FactoriesSidebarNav({
  organizationId,
  factoryKey,
  lineId,
  showAgent = false,
  showVelocity = true,
}: FactoriesSidebarNavProps) {
  const { pathname } = useLocation();
  const boardHref = factoryHomePath(organizationId, factoryKey, lineId);
  const agentHref = factoryAgentPath(organizationId, factoryKey);
  const velocityHref = factoryVelocityPath(organizationId, factoryKey);
  const automationsHref = automationsPath(organizationId, factoryKey);

  return (
    <nav className="flex flex-col items-center gap-1 px-1.5" aria-label="Workspace" data-testid="factories-sidebar-nav">
      <RailNavLink
        to={boardHref}
        label="Board"
        Icon={Home}
        testId="factories-nav-board"
        isCurrent={isBoardPath(pathname)}
      />
      {showAgent ? (
        <RailNavLink
          to={agentHref}
          label="Agent"
          Icon={Cpu}
          testId="factories-nav-agent"
          isCurrent={isAgentPath(pathname)}
        />
      ) : null}
      {showAgent ? (
        <RailNavLink
          to={automationsHref}
          label="Automations"
          Icon={Workflow}
          testId="factories-nav-automations"
          isCurrent={isAutomationsPath(pathname)}
        />
      ) : null}
      {showVelocity ? (
        <RailNavLink
          to={velocityHref}
          label="Velocity"
          Icon={ChartLine}
          testId="factories-nav-velocity"
          isCurrent={isVelocityPath(pathname)}
        />
      ) : null}
    </nav>
  );
}
