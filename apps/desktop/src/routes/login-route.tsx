import { useState } from "react";
import { useNavigate } from "react-router-dom";
import mylogo from "@web/public/desktop/mylogo.png";
import { WORKSPACE_STORAGE_KEY } from "@myteam/client-core";
import { useDesktopAuthStore, useDesktopWorkspaceStore } from "@/lib/desktop-client";

export function LoginRoute() {
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [step, setStep] = useState<"email" | "verify">("email");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const handleSendCode = async () => {
    setIsSubmitting(true);
    setError(null);
    setNotice(null);
    try {
      await window.myteam.auth.sendCode(email);
      setStep("verify");
      setNotice(`Verification code sent to ${email.trim().toLowerCase()}`);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Failed to send code");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleVerifyCode = async () => {
    setIsSubmitting(true);
    setError(null);
    setNotice(null);
    try {
      const session = await window.myteam.auth.verifyCode(email, code);
      await useDesktopAuthStore.getState().setSession(session.token, session.user);
      const preferredWorkspaceId =
        await window.myteam.shell.getPreference(WORKSPACE_STORAGE_KEY);
      await useDesktopWorkspaceStore.getState().bootstrap(preferredWorkspaceId);
      navigate("/session", { replace: true });
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Login failed");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6">
      <div className="w-full max-w-xl rounded-[28px] border border-border/80 bg-card/90 p-10 shadow-2xl shadow-black/30 backdrop-blur-xl">
        <div className="flex items-center gap-4">
          <img src={mylogo} alt="MyTeam" className="h-14 w-14 rounded-2xl object-contain" />
          <div>
            <p className="text-xs uppercase tracking-[0.24em] text-muted-foreground">
              MyTeam Desktop
            </p>
            <h1 className="mt-2 text-3xl font-semibold text-foreground">
              Replace the browser shell with a native desktop workspace.
            </h1>
          </div>
        </div>

        <p className="mt-6 text-base leading-7 text-muted-foreground">
          Sign in with your MyTeam account directly in the desktop client. We will
          email you a verification code, exchange it for a session token, and store
          the resulting PAT in the macOS keychain through the native helper.
        </p>

        <div className="mt-8 rounded-3xl border border-border/70 bg-background/80 p-5">
          <p className="text-sm font-medium text-foreground">What gets initialized</p>
          <ul className="mt-3 space-y-2 text-sm text-muted-foreground">
            <li>Secure PAT storage in Keychain</li>
            <li>Workspace hydration from the current MyTeam backend</li>
            <li>Desktop runtime control via the local `myteam` CLI</li>
          </ul>
        </div>

        <div className="mt-8 space-y-4">
          <div className="space-y-2">
            <label htmlFor="email" className="text-sm font-medium text-foreground">
              Email
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              disabled={isSubmitting || step === "verify"}
              placeholder="you@myteam.ai"
              className="h-12 w-full rounded-2xl border border-border/80 bg-background px-4 text-sm text-foreground outline-none transition focus:border-primary disabled:cursor-not-allowed disabled:opacity-70"
            />
          </div>

          {step === "verify" ? (
            <div className="space-y-2">
              <label htmlFor="code" className="text-sm font-medium text-foreground">
                Verification code
              </label>
              <input
                id="code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                value={code}
                onChange={(event) => setCode(event.target.value)}
                disabled={isSubmitting}
                placeholder="123456"
                className="h-12 w-full rounded-2xl border border-border/80 bg-background px-4 text-sm tracking-[0.32em] text-foreground outline-none transition focus:border-primary disabled:cursor-not-allowed disabled:opacity-70"
              />
            </div>
          ) : null}
        </div>

        {notice ? (
          <div className="mt-6 rounded-2xl border border-primary/30 bg-primary/10 px-4 py-3 text-sm text-primary-foreground">
            {notice}
          </div>
        ) : null}

        {error ? (
          <div className="mt-6 rounded-2xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">
            {error}
          </div>
        ) : null}

        <div className="mt-8 flex flex-col gap-3 sm:flex-row">
          {step === "verify" ? (
            <button
              type="button"
              onClick={() => {
                setStep("email");
                setCode("");
                setError(null);
                setNotice(null);
              }}
              disabled={isSubmitting}
              className="inline-flex h-12 items-center justify-center rounded-2xl border border-border/80 bg-background px-4 text-sm font-medium text-foreground transition hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60 sm:w-40"
            >
              Change email
            </button>
          ) : null}

          <button
            type="button"
            onClick={() => void (step === "email" ? handleSendCode() : handleVerifyCode())}
            disabled={isSubmitting || !email.trim() || (step === "verify" && !code.trim())}
            className="inline-flex h-12 flex-1 items-center justify-center rounded-2xl bg-primary px-4 text-sm font-medium text-primary-foreground transition hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isSubmitting
              ? step === "email"
                ? "Sending code…"
                : "Signing in…"
              : step === "email"
                ? "Send verification code"
                : "Sign in to MyTeam"}
          </button>
        </div>
      </div>
    </div>
  );
}
