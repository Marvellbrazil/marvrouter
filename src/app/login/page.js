"use client";

import { useState, useEffect } from "react";
import { Card, Button, Input } from "@/shared/components";

export default function LoginPage() {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [useRecovery, setUseRecovery] = useState(false);
  const [step2fa, setStep2fa] = useState(false);
  const [error, setError] = useState("");
  const [resetHint, setResetHint] = useState("");
  const [retryAfter, setRetryAfter] = useState(0);
  const [loading, setLoading] = useState(false);
  const [hasPassword, setHasPassword] = useState(null);
  const [authMode, setAuthMode] = useState("password");
  const [ssoType, setSsoType] = useState("oidc");
  const [oidcConfigured, setOidcConfigured] = useState(false);
  const [oidcLoginLabel, setOidcLoginLabel] = useState("Sign in with OIDC");
  const [samlConfigured, setSamlConfigured] = useState(false);
  const [samlLoginLabel, setSamlLoginLabel] = useState("Sign in with SAML SSO");
  const [mustChange, setMustChange] = useState(false);
  const [newPassword, setNewPassword] = useState("");

  useEffect(() => {
    if (retryAfter <= 0) return;
    const id = setInterval(
      () => setRetryAfter((s) => (s > 0 ? s - 1 : 0)),
      1000,
    );
    return () => clearInterval(id);
  }, [retryAfter]);

  useEffect(() => {
    async function checkAuth() {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 5000);
      const baseUrl =
        typeof window !== "undefined" ? window.location.origin : "";

      try {
        const res = await fetch(`${baseUrl}/api/auth/status`, {
          signal: controller.signal,
        });
        clearTimeout(timeoutId);

        if (res.ok) {
          const data = await res.json();
          if (data.authenticated === true || data.requireLogin === false) {
            window.location.assign("/dashboard");
            return;
          }
          if (data.username) {
            setUsername(data.username);
          }
          setHasPassword(!!data.hasPassword);
          setAuthMode(data.authMode || "password");
          setSsoType(data.ssoType || "oidc");
          setOidcConfigured(data.oidcConfigured === true);
          setOidcLoginLabel(data.oidcLoginLabel || "Sign in with OIDC");
          setSamlConfigured(data.samlConfigured === true);
          setSamlLoginLabel(data.samlLoginLabel || "Sign in with SAML SSO");
        } else {
          setHasPassword(true);
        }
      } catch {
        clearTimeout(timeoutId);
        setHasPassword(true);
      }
    }
    checkAuth();
  }, []);

  const handleLogin = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    setResetHint("");

    try {
      const payload = {
        username: username.trim(),
        password,
      };

      if (step2fa) {
        if (useRecovery) {
          payload.recoveryCode = recoveryCode.trim();
        } else {
          payload.totpCode = totpCode.trim();
        }
      }

      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      const data = await res.json();

      if (res.ok) {
        if (data.require2fa) {
          setStep2fa(true);
          setLoading(false);
          return;
        }
        if (data.mustChangePassword) {
          setMustChange(true);
          return;
        }
        window.location.assign("/dashboard");
      } else {
        setError(data.error || "Invalid credentials");
        if (data.resetHint) setResetHint(data.resetHint);
        if (data.retryAfter) setRetryAfter(Number(data.retryAfter));
      }
    } catch {
      setError("An error occurred. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  const handleSetNewPassword = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ currentPassword: password, newPassword }),
      });
      if (res.ok) {
        window.location.assign("/dashboard");
      } else {
        const data = await res.json();
        setError(data.error || "Failed to set password");
      }
    } catch {
      setError("An error occurred. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  const handleOidcLogin = () => {
    window.location.href = "/api/auth/oidc/start";
  };

  const handleSamlLogin = () => {
    window.location.href = "/api/auth/saml/start";
  };

  const isSsoEnabled = ["sso", "oidc", "saml", "both"].includes(authMode);
  const activeSsoType = ssoType || (authMode === "saml" ? "saml" : "oidc");

  const samlAvailable =
    isSsoEnabled && activeSsoType === "saml" && samlConfigured;
  const oidcAvailable =
    isSsoEnabled && activeSsoType === "oidc" && oidcConfigured;
  const ssoAvailable = samlAvailable || oidcAvailable;

  const passwordAvailable =
    authMode === "password" || authMode === "both" || !ssoAvailable;

  if (hasPassword === null) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-bg p-4">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
          <p className="text-text-muted mt-4">Loading...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg p-4 relative overflow-hidden">
      <div
        className="landing-grid absolute inset-0 pointer-events-none"
        aria-hidden="true"
      />
      <div className="relative z-10 w-full max-w-md">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-primary mb-2">9Router</h1>
          <p className="text-text-muted">
            {step2fa
              ? "Two-factor authentication required"
              : samlAvailable
                ? "Sign in with SAML 2.0 Single Sign-On"
                : oidcAvailable
                  ? "Sign in with your OIDC provider to access the dashboard"
                  : "Enter your credentials to access the dashboard"}
          </p>
        </div>

        <Card>
          {mustChange ? (
            <form
              onSubmit={handleSetNewPassword}
              className="flex flex-col gap-4"
            >
              <p className="text-sm text-amber-600 dark:text-amber-400 text-center">
                Set a new password before accessing the dashboard remotely.
              </p>
              <div className="flex flex-col gap-2">
                <label className="text-sm font-medium">New password</label>
                <Input
                  type="password"
                  placeholder="Enter new password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                  autoFocus
                />
                {error && <p className="text-xs text-red-500">{error}</p>}
              </div>
              <Button
                type="submit"
                variant="primary"
                className="w-full"
                loading={loading}
                disabled={!newPassword}
              >
                Set password
              </Button>
            </form>
          ) : step2fa ? (
            <form onSubmit={handleLogin} className="flex flex-col gap-4">
              <p className="text-sm text-text-muted text-center">
                {useRecovery
                  ? "Enter one of your 8-character recovery backup codes."
                  : "Enter the 6-digit code generated by your Authenticator app."}
              </p>

              {useRecovery ? (
                <div className="flex flex-col gap-2">
                  <label className="text-sm font-medium">Recovery code</label>
                  <Input
                    type="text"
                    placeholder="e.g. 2A3B4C5D"
                    value={recoveryCode}
                    onChange={(e) =>
                      setRecoveryCode(e.target.value.toUpperCase())
                    }
                    required
                    autoFocus
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-2">
                  <label className="text-sm font-medium">
                    Authenticator code
                  </label>
                  <Input
                    type="text"
                    inputMode="numeric"
                    maxLength={6}
                    placeholder="123456"
                    value={totpCode}
                    onChange={(e) =>
                      setTotpCode(e.target.value.replace(/\D/g, ""))
                    }
                    required
                    autoFocus
                  />
                </div>
              )}

              {error && <p className="text-xs text-red-500">{error}</p>}

              <Button
                type="submit"
                variant="primary"
                className="w-full"
                loading={loading}
                disabled={retryAfter > 0}
              >
                Verify & Login
              </Button>

              <div className="flex items-center justify-between text-xs text-text-muted mt-2">
                <button
                  type="button"
                  className="hover:text-primary transition-colors cursor-pointer"
                  onClick={() => {
                    setUseRecovery(!useRecovery);
                    setError("");
                  }}
                >
                  {useRecovery
                    ? "Use Authenticator app code"
                    : "Use a backup recovery code"}
                </button>
                <button
                  type="button"
                  className="hover:text-primary transition-colors cursor-pointer"
                  onClick={() => {
                    setStep2fa(false);
                    setTotpCode("");
                    setRecoveryCode("");
                    setError("");
                  }}
                >
                  Back to credentials
                </button>
              </div>
            </form>
          ) : (
            <div className="flex flex-col gap-4">
              {samlAvailable && (
                <Button
                  type="button"
                  variant="primary"
                  className="w-full"
                  onClick={handleSamlLogin}
                >
                  {samlLoginLabel}
                </Button>
              )}

              {oidcAvailable && (
                <Button
                  type="button"
                  variant="primary"
                  className="w-full"
                  onClick={handleOidcLogin}
                >
                  {oidcLoginLabel}
                </Button>
              )}

              {ssoAvailable && passwordAvailable && (
                <div className="h-px bg-border/60" />
              )}

              {passwordAvailable ? (
                <form onSubmit={handleLogin} className="flex flex-col gap-4">
                  {isSsoEnabled && !ssoAvailable && (
                    <p className="text-xs text-amber-600 dark:text-amber-400 text-center">
                      {activeSsoType === "saml" ? "SAML SSO" : "OIDC"} login is
                      enabled, but configuration is incomplete. Password login
                      is still available for recovery.
                    </p>
                  )}

                  {authMode === "both" && ssoAvailable && (
                    <p className="text-xs text-text-muted text-center">
                      Password and{" "}
                      {activeSsoType === "saml" ? "SAML SSO" : "OIDC"} login are
                      both enabled.
                    </p>
                  )}

                  <div className="flex flex-col gap-2">
                    <label className="text-sm font-medium">Username</label>
                    <Input
                      type="text"
                      placeholder="admin"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      required
                    />
                  </div>

                  <div className="flex flex-col gap-2">
                    <label className="text-sm font-medium">Password</label>
                    <Input
                      type="password"
                      placeholder="Enter password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      required
                      autoFocus={!oidcAvailable}
                    />
                    {error && <p className="text-xs text-red-500">{error}</p>}
                    {retryAfter > 0 && (
                      <p className="text-xs text-amber-600 dark:text-amber-400">
                        Locked. Retry in{" "}
                        <span className="font-mono">{retryAfter}s</span>.
                      </p>
                    )}
                    {resetHint && (
                      <p className="text-xs text-text-muted">
                        Forgot password? Open{" "}
                        <code className="bg-sidebar px-1 rounded">9router</code>{" "}
                        CLI on the host → <b>Settings</b> →{" "}
                        <b>Reset Password to Default</b>.
                      </p>
                    )}
                  </div>

                  <Button
                    type="submit"
                    variant="primary"
                    className="w-full"
                    loading={loading}
                    disabled={retryAfter > 0}
                  >
                    {retryAfter > 0 ? `Wait ${retryAfter}s` : "Login"}
                  </Button>

                  <p className="text-xs text-center text-text-muted mt-2">
                    Default credentials are{" "}
                    <code className="bg-sidebar px-1 rounded">admin</code> /{" "}
                    <code className="bg-sidebar px-1 rounded">123456</code>
                  </p>
                  {hasPassword === false && (
                    <p className="text-xs text-center text-amber-600 dark:text-amber-400">
                      Security risk: no password set. You will be asked to set
                      one when logging in remotely.
                    </p>
                  )}
                </form>
              ) : (
                error && <p className="text-xs text-red-500">{error}</p>
              )}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
