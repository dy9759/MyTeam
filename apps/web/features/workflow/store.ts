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
  retryStep: (workflowId: string, stepId: string) => Promise<void>;
  replaceStepAgent: (workflowId: string, stepId: string, agentId: string) => Promise<void>;
  updateStepInWorkflow: (workflowId: string, stepId: string, updates: Partial<Workflow["steps"] extends (infer S)[] | undefined ? S : never>) => void;
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
      toast.error("加载工作流失败");
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
      toast.error("加载计划失败");
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
      toast.error("生成计划失败");
      return null;
    }
  },

  retryStep: async (workflowId: string, stepId: string) => {
    try {
      await api.retryWorkflowStep(workflowId, stepId);
      logger.info("retry step", stepId);
      toast.success("已触发重试");
    } catch (err) {
      logger.error("retry step failed", err);
      toast.error("重试步骤失败");
    }
  },

  replaceStepAgent: async (workflowId: string, stepId: string, agentId: string) => {
    try {
      await api.replaceStepAgent(workflowId, stepId, agentId);
      logger.info("replace step agent", stepId, agentId);
      toast.success("已替换Agent");
    } catch (err) {
      logger.error("replace step agent failed", err);
      toast.error("替换Agent失败");
    }
  },

  updateStepInWorkflow: (workflowId: string, stepId: string, updates: Record<string, unknown>) => {
    set((s) => ({
      workflows: s.workflows.map((w) => {
        if (w.id !== workflowId || !w.steps) return w;
        return {
          ...w,
          steps: w.steps.map((step) =>
            step.id === stepId ? { ...step, ...updates } : step,
          ),
        };
      }),
    }));
  },
}));
