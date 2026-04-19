"use client";

import { Suspense, useState, useEffect, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
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
    if (!isLoading && user && !searchParams.get("cli_callback")) {
      router.replace(searchParams.get("next") || "/issues");
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
      setError("请输入邮箱地址");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      await sendCode(email);
      setStep("code");
      setCode("");
      setCooldown(10);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "发送验证码失败，请确保服务器正在运行。"
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleVerifyCode = useCallback(
    async (value: string) => {
      if (value.length !== 6) return;
      setError("");
      setSubmitting(true);
      try {
        const cliCallback = searchParams.get("cli_callback");
        if (cliCallback) {
          if (!validateCliCallback(cliCallback)) {
            setError("无效的回调地址");
            setSubmitting(false);
            return;
          }
          const { token } = await api.verifyCode(email, value);
          const cliState = searchParams.get("cli_state") || "";
          redirectToCliCallback(cliCallback, token, cliState);
          return;
        }

        await verifyCode(email, value);
        const wsList = await api.listWorkspaces();
        await hydrateWorkspace(wsList);
        router.push(searchParams.get("next") || "/issues");
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "验证码无效或已过期"
        );
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
        err instanceof Error ? err.message : "重新发送验证码失败"
      );
    }
  };

  // CLI confirm step: user is already logged in, just authorize.
  if (step === "cli_confirm" && existingUser) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <Card className="w-full max-w-sm rounded-xl">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">授权 CLI</CardTitle>
            <CardDescription>
              允许 CLI 以{" "}
              <span className="font-medium text-foreground">
                {existingUser.email}
              </span>{" "}
              的身份访问 My Team？
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Button
              onClick={handleCliAuthorize}
              disabled={submitting}
              className="w-full bg-primary text-primary-foreground hover:bg-primary/90"
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
              使用其他账户
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (step === "code") {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <Card className="w-full max-w-sm rounded-xl">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">查看你的邮箱</CardTitle>
            <CardDescription>
              我们已将验证码发送至{" "}
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
                {cooldown > 0 ? `${cooldown}秒后重新发送` : "重新发送验证码"}
              </button>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              variant="ghost"
              className="w-full text-primary"
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
    <div className="flex min-h-screen items-center justify-center bg-background">
      <Card className="w-full max-w-sm rounded-xl">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl font-bold">My Team</CardTitle>
          <CardDescription>让编程 Agent 成为真正的队友</CardDescription>
        </CardHeader>
        <CardContent>
          <form id="login-form" onSubmit={handleSendCode} className="space-y-4">
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
          </form>
        </CardContent>
        <CardFooter>
          <Button
            type="submit"
            form="login-form"
            disabled={submitting}
            className="w-full bg-primary text-primary-foreground hover:bg-primary/90"
            size="lg"
          >
            {submitting ? "发送验证码中..." : "继续"}
          </Button>
        </CardFooter>
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
