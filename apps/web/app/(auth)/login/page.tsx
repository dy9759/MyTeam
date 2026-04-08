"use client";

import { Suspense, useState, useEffect, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const log = createLogger("auth:login");
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from "@/components/ui/input-otp";
import type { User } from "@/shared/types";

function validateCliCallback(cliCallback: string): boolean {
  try {
    const cbUrl = new URL(cliCallback);
    if (cbUrl.protocol !== "http:") return false;
    if (cbUrl.hostname !== "localhost" && cbUrl.hostname !== "127.0.0.1")
      return false;
    return true;
  } catch {
    return false;
  }
}

function redirectToCliCallback(
  cliCallback: string,
  token: string,
  cliState: string
) {
  const separator = cliCallback.includes("?") ? "&" : "?";
  window.location.href = `${cliCallback}${separator}token=${encodeURIComponent(token)}&state=${encodeURIComponent(cliState)}`;
}

function LoginPageContent() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const sendCode = useAuthStore((s) => s.sendCode);
  const verifyCode = useAuthStore((s) => s.verifyCode);
  const hydrateWorkspace = useWorkspaceStore((s) => s.hydrateWorkspace);
  const searchParams = useSearchParams();

  // Already authenticated — redirect to dashboard
  useEffect(() => {
    log.debug("auth state check", { isLoading, hasUser: !!user, cliCallback: searchParams.get("cli_callback") });
    if (!isLoading && user && !searchParams.get("cli_callback")) {
      const dest = searchParams.get("next") || "/issues";
      log.info("already authenticated, redirecting", { dest, userId: user.id });
      router.replace(dest);
    }
  }, [isLoading, user, router, searchParams]);

  const [step, setStep] = useState<"email" | "code" | "cli_confirm">("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [existingUser, setExistingUser] = useState<User | null>(null);

  // Check for existing session when CLI callback is present.
  useEffect(() => {
    const cliCallback = searchParams.get("cli_callback");
    if (!cliCallback) return;

    const token = localStorage.getItem("multica_token");
    if (!token) return;

    if (!validateCliCallback(cliCallback)) return;

    // Verify the existing token is still valid.
    api.setToken(token);
    api
      .getMe()
      .then((user) => {
        setExistingUser(user);
        setStep("cli_confirm");
      })
      .catch(() => {
        // Token expired/invalid — clear and fall through to normal login.
        api.setToken(null);
        localStorage.removeItem("multica_token");
      });
  }, [searchParams]);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  const handleCliAuthorize = async () => {
    const cliCallback = searchParams.get("cli_callback");
    const token = localStorage.getItem("multica_token");
    if (!cliCallback || !token) return;
    const cliState = searchParams.get("cli_state") || "";
    setSubmitting(true);
    redirectToCliCallback(cliCallback, token, cliState);
  };

  const handleSendCode = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!email) {
      setError("请输入邮箱");
      return;
    }
    setError("");
    setSubmitting(true);
    log.info("handleSendCode: sending code", { email });
    try {
      await sendCode(email);
      log.info("handleSendCode: code sent, transitioning to code step");
      setStep("code");
      setCode("");
      setCooldown(10);
    } catch (err) {
      const msg = err instanceof Error
        ? err.message
        : "发送验证码失败，请确保服务器正在运行。";
      log.error("handleSendCode: failed", { email, error: msg });
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  const handleVerifyCode = useCallback(
    async (value: string) => {
      if (value.length !== 6) return;
      setError("");
      setSubmitting(true);
      log.info("handleVerifyCode: verifying", { email, codeLength: value.length });
      try {
        const cliCallback = searchParams.get("cli_callback");
        if (cliCallback) {
          log.info("handleVerifyCode: CLI flow detected", { cliCallback });
          if (!validateCliCallback(cliCallback)) {
            log.error("handleVerifyCode: invalid CLI callback URL", { cliCallback });
            setError("无效的回调 URL");
            setSubmitting(false);
            return;
          }
          const { token } = await api.verifyCode(email, value);
          const cliState = searchParams.get("cli_state") || "";
          log.info("handleVerifyCode: CLI verify success, redirecting to callback");
          redirectToCliCallback(cliCallback, token, cliState);
          return;
        }

        await verifyCode(email, value);
        log.info("handleVerifyCode: verify success, hydrating workspace");
        const wsList = await api.listWorkspaces();
        log.info("handleVerifyCode: got workspaces", { count: wsList.length });
        await hydrateWorkspace(wsList);
        const dest = searchParams.get("next") || "/issues";
        log.info("handleVerifyCode: login complete, navigating", { dest });
        router.push(dest);
      } catch (err) {
        const msg = err instanceof Error ? err.message : "验证码无效或已过期";
        log.error("handleVerifyCode: failed", { email, error: msg });
        setError(msg);
        setCode("");
        setSubmitting(false);
      }
    },
    [email, verifyCode, hydrateWorkspace, router, searchParams]
  );

  const handleResend = async () => {
    if (cooldown > 0) return;
    setError("");
    try {
      await sendCode(email);
      setCooldown(10);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "重发验证码失败"
      );
    }
  };

  // CLI confirm step: user is already logged in, just authorize.
  if (step === "cli_confirm" && existingUser) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Authorize CLI</CardTitle>
            <CardDescription>
              Allow the CLI to access My Team as{" "}
              <span className="font-medium text-foreground">
                {existingUser.email}
              </span>
              ?
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Button
              onClick={handleCliAuthorize}
              disabled={submitting}
              className="w-full"
              size="lg"
            >
              {submitting ? "授权中..." : "授权"}
            </Button>
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                setExistingUser(null);
                setStep("email");
              }}
            >
              Use a different account
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (step === "code") {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">查看邮箱</CardTitle>
            <CardDescription>
              验证码已发送至{" "}
              <span className="font-medium text-foreground">{email}</span>
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-4">
            <InputOTP
              maxLength={6}
              value={code}
              onChange={(value) => {
                setCode(value);
                if (value.length === 6) handleVerifyCode(value);
              }}
              disabled={submitting}
            >
              <InputOTPGroup>
                <InputOTPSlot index={0} />
                <InputOTPSlot index={1} />
                <InputOTPSlot index={2} />
                <InputOTPSlot index={3} />
                <InputOTPSlot index={4} />
                <InputOTPSlot index={5} />
              </InputOTPGroup>
            </InputOTP>
            {error && (
              <p className="text-sm text-destructive">{error}</p>
            )}
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <button
                type="button"
                onClick={handleResend}
                disabled={cooldown > 0}
                className="text-primary underline-offset-4 hover:underline disabled:text-muted-foreground disabled:no-underline disabled:cursor-not-allowed"
              >
                {cooldown > 0 ? `${cooldown}秒后重发` : "重新发送"}
              </button>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                setStep("email");
                setCode("");
                setError("");
              }}
            >
              返回
            </Button>
          </CardFooter>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">My Team</CardTitle>
          <CardDescription>让编程代理成为真正的队友</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSendCode} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">邮箱</Label>
              <Input
                id="email"
                type="email"
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            {error && (
              <p className="text-sm text-destructive">{error}</p>
            )}
            <Button
              type="submit"
              disabled={submitting}
              className="w-full"
              size="lg"
            >
              {submitting ? "发送中..." : "继续"}
            </Button>
          </form>
        </CardContent>
        <CardFooter />
      </Card>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}
