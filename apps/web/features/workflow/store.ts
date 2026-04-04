"use client";

import { create } from "zustand";
import type { Workflow, Plan } from "@/shared/types/workflow";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const logger = createLogger("workflow-store");

interface WorkflowState {
  workflows: Workflow[];
  plans: Plan[];
  currentWorkflow: Workflow | null;
  loading: boolean;
  fetchWorkflows: () => Promise<void>;
  fetchPlans: () => Promise<void>;
  setCurrentWorkflow: (w: Workflow | null) => void;
  generatePlan: (input: string) => Promise<Plan | null>;
}

export const useWorkflowStore = create<WorkflowState>((set, get) => ({
  workflows: [],
  plans: [],
  currentWorkflow: null,
  loading: true,

  fetchWorkflows: async () => {
    logger.debug("fetch workflows start");
    const isInitialLoad = get().workflows.length === 0;
    if (isInitialLoad) set({ loading: true });
    try {
      const res = await api.listWorkflows(200);
      logger.info("fetched", res.workflows.length, "workflows");
      set({ workflows: res.workflows as Workflow[], loading: false });
    } catch (err) {
      logger.error("fetch workflows failed", err);
      toast.error("Failed to load workflows");
      if (isInitialLoad) set({ loading: false });
    }
  },

  fetchPlans: async () => {
    logger.debug("fetch plans start");
    try {
      const res = await api.listPlans(200);
      logger.info("fetched", res.plans.length, "plans");
      set({ plans: res.plans as Plan[] });
    } catch (err) {
      logger.error("fetch plans failed", err);
      toast.error("Failed to load plans");
    }
  },

  setCurrentWorkflow: (w) => set({ currentWorkflow: w }),

  generatePlan: async (input: string) => {
    try {
      const plan = await api.generatePlan(input);
      logger.info("generated plan", plan.id);
      set((s) => ({ plans: [...s.plans, plan as Plan] }));
      return plan as Plan;
    } catch (err) {
      logger.error("generate plan failed", err);
      toast.error("Failed to generate plan");
      return null;
    }
  },
}));
