import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeftRight, KeyRound, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FieldError } from "@/components/bits";
import { qk } from "@/hooks/queries";
import { api, ApiError } from "@/lib/api";

function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-muted/40 p-4">
      <div className="mb-6 flex items-center gap-2">
        <div className="flex size-9 items-center justify-center rounded-lg bg-foreground text-background">
          <ArrowLeftRight className="size-4" />
        </div>
        <span className="text-xl font-semibold tracking-tight">proxier</span>
      </div>
      <div className="w-full max-w-sm">{children}</div>
    </div>
  );
}

export function LoginPage() {
  const qc = useQueryClient();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/auth/login", { body: { password } });
      await qc.invalidateQueries({ queryKey: qk.auth });
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "Wrong password" : err instanceof Error ? err.message : "Login failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout>
      <Card>
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>Enter the admin password.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus />
              <FieldError msg={error} />
            </div>
            <Button type="submit" className="w-full" disabled={busy || !password}>
              {busy && <Loader2 className="animate-spin" />}
              Sign in
            </Button>
            <p className="text-xs text-muted-foreground">
              Forgot it? On the server run <code className="rounded bg-muted px-1">proxier reset-password</code>.
            </p>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  );
}

export function SetupPage() {
  const qc = useQueryClient();
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password !== confirm) {
      setErrors({ confirm: "Passwords don't match" });
      return;
    }
    setBusy(true);
    setErrors({});
    try {
      await api("/auth/setup", { body: { token, password } });
      await qc.invalidateQueries({ queryKey: qk.auth });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) setErrors({ token: "Invalid setup token" });
        else if (err.fields) setErrors(err.fields);
        else setErrors({ token: err.message });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="size-4" /> First-time setup
          </CardTitle>
          <CardDescription>
            Enter the setup token printed in the server log (or in <code className="text-xs">setup-token.txt</code> in the
            data directory) and choose an admin password.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="token">Setup token</Label>
              <Input id="token" value={token} onChange={(e) => setToken(e.target.value)} className="font-mono uppercase" autoComplete="off" autoFocus />
              <FieldError msg={errors.token} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pw">Admin password</Label>
              <Input id="pw" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
              <p className="text-xs text-muted-foreground">At least 10 characters.</p>
              <FieldError msg={errors.password} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pw2">Confirm password</Label>
              <Input id="pw2" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
              <FieldError msg={errors.confirm} />
            </div>
            <Button type="submit" className="w-full" disabled={busy || !token || !password}>
              {busy && <Loader2 className="animate-spin" />}
              Create admin
            </Button>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  );
}
