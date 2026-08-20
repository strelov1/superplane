import type { InstallParam } from "@/pages/install/types";

import type { FactoryDefinition } from "./types";
import factoryMeta from "./software-factory/factory.json";
import factoryParams from "./software-factory/params.json";
import softwareFactoryCanvasYaml from "./software-factory/canvas.yaml?raw";
import softwareFactoryConsoleYaml from "./software-factory/console.yaml?raw";
import contributionOnboardingMeta from "./contribution-onboarding/factory.json";
import contributionOnboardingParams from "./contribution-onboarding/params.json";
import contributionOnboardingCanvasYaml from "./contribution-onboarding/canvas.yaml?raw";
import contributionOnboardingConsoleYaml from "./contribution-onboarding/console.yaml?raw";

export type { FactoryDefinition, FactoryStartingTask, FactoryRunDefinition } from "./types";
export {
  buildFactoryRunParameters,
  materializeFactoryCanvas,
  materializeFactoryConsole,
  substituteInstallParams,
  wireFactoryIntegrations,
} from "./materializeFactoryTemplate";

function buildSoftwareFactory(): FactoryDefinition {
  return {
    id: factoryMeta.id,
    title: factoryMeta.title,
    description: factoryMeta.description,
    integrations: factoryMeta.integrations,
    componentIntegrations: factoryMeta.componentIntegrations,
    startingTasks: factoryMeta.startingTasks,
    agentSuggestions: factoryMeta.agentSuggestions,
    run: factoryMeta.run as FactoryDefinition["run"],
    source: factoryMeta.source as FactoryDefinition["source"],
    installParams: factoryParams.install_params as InstallParam[],
    canvasYaml: softwareFactoryCanvasYaml,
    consoleYaml: softwareFactoryConsoleYaml,
  };
}

function buildContributionOnboarding(): FactoryDefinition {
  return {
    id: contributionOnboardingMeta.id,
    title: contributionOnboardingMeta.title,
    description: contributionOnboardingMeta.description,
    integrations: contributionOnboardingMeta.integrations,
    componentIntegrations: contributionOnboardingMeta.componentIntegrations,
    startingTasks: contributionOnboardingMeta.startingTasks,
    agentSuggestions: contributionOnboardingMeta.agentSuggestions,
    run: contributionOnboardingMeta.run as FactoryDefinition["run"],
    source: contributionOnboardingMeta.source as FactoryDefinition["source"],
    installParams: contributionOnboardingParams.install_params as InstallParam[],
    canvasYaml: contributionOnboardingCanvasYaml,
    consoleYaml: contributionOnboardingConsoleYaml,
  };
}

const FACTORY_BY_ID: Record<string, FactoryDefinition> = {
  "software-factory": buildSoftwareFactory(),
  "contribution-onboarding": buildContributionOnboarding(),
};

export const DEFAULT_FACTORY_ID = "software-factory";

export function getFactoryDefinition(id: string = DEFAULT_FACTORY_ID): FactoryDefinition {
  const definition = FACTORY_BY_ID[id];
  if (!definition) {
    throw new Error(`Unknown factory definition: ${id}`);
  }
  return definition;
}

export function listFactoryDefinitions(): FactoryDefinition[] {
  return Object.values(FACTORY_BY_ID);
}
