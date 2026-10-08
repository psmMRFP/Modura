import { useState } from "react";
import { Alert, Button, Form, Input, Space } from "antd";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import {
  useGetConsumerAuthStatus,
  useRegisterConsumer,
  useResendConsumerVerification,
  useRequestConsumerRecovery,
  useVerifyConsumerEmail,
  useResetConsumerPassword,
  useLoginConsumer,
  useRefreshConsumer,
  useLogoutConsumer,
  useGetConsumerProfile,
  type ConsumerRegistrationRequest,
  type ConsumerResetRequest,
} from "../../api/generated/places";
import { isLocale } from "../../app/locales";
import { authRoutes } from "./routes";
import { authMessages } from "./messages";
import { Challenge } from "./Challenge";
import { consumerCSRF, useConsumerSession } from "./session";

export function AuthPage() {
  const { locale: requested } = useParams();
  const locale = isLocale(requested) ? requested : "en";
  const copy = authMessages[locale];
  const location = useLocation();
  const navigate = useNavigate();
  const mode = location.pathname.split("/").at(-1);
  const session = useConsumerSession();
  const status = useGetConsumerAuthStatus({ query: { retry: false } });
  const [form] =
    Form.useForm<Partial<ConsumerRegistrationRequest & ConsumerResetRequest>>();
  const [challenge, setChallenge] = useState("");
  const [generation, setGeneration] = useState(0);
  const [result, setResult] = useState<
    "accepted" | "completed" | "failure" | null
  >(null);
  const registration = useRegisterConsumer({ mutation: { gcTime: 0 } });
  const resend = useResendConsumerVerification({ mutation: { gcTime: 0 } });
  const recover = useRequestConsumerRecovery({ mutation: { gcTime: 0 } });
  const verify = useVerifyConsumerEmail({ mutation: { gcTime: 0 } });
  const reset = useResetConsumerPassword({ mutation: { gcTime: 0 } });
  const login = useLoginConsumer({ mutation: { gcTime: 0 } });
  const refresh = useRefreshConsumer({
    mutation: { gcTime: 0 },
    fetch: {
      credentials: "same-origin",
      headers: { "X-CSRF-Token": consumerCSRF() },
    },
  });
  const logout = useLogoutConsumer({
    mutation: { gcTime: 0 },
    fetch: {
      credentials: "same-origin",
      headers: {
        Authorization: `Bearer ${session.access}`,
        "X-CSRF-Token": consumerCSRF(),
      },
    },
  });
  const profile = useGetConsumerProfile({
    query: { enabled: !!session.access, retry: false, gcTime: 0 },
    fetch: { headers: { Authorization: `Bearer ${session.access}` } },
  });
  const busy = [
    registration,
    resend,
    recover,
    verify,
    reset,
    login,
    refresh,
    logout,
  ].some((mutation) => mutation.isPending);
  const titles: Record<string, string> = {
    login: copy.login,
    register: copy.register,
    "resend-verification": copy.resend,
    recover: copy.recover,
    "verify-email": copy.verify,
    "reset-password": copy.reset,
    account: copy.account,
  };
  async function submit(
    values: Partial<ConsumerRegistrationRequest & ConsumerResetRequest>,
  ) {
    setResult(null);
    try {
      const email = values.email ?? "",
        password = values.password ?? "",
        code = values.code ?? "";
      if (mode === "login") {
        const response = await login.mutateAsync({
          data: { email, password, challenge },
        });
        if (response.status !== 200) throw new Error("request failed");
        session.replace(response.data.accessToken);
        void navigate(`/${locale}/account`);
      } else {
        const response =
          mode === "register"
            ? await registration.mutateAsync({
                data: { email, password, challenge },
              })
            : mode === "resend-verification"
              ? await resend.mutateAsync({ data: { email, challenge } })
              : mode === "recover"
                ? await recover.mutateAsync({ data: { email, challenge } })
                : mode === "verify-email"
                  ? await verify.mutateAsync({ data: { code, challenge } })
                  : await reset.mutateAsync({
                      data: { code, password, challenge },
                    });
        if (response.status !== 202 && response.status !== 204)
          throw new Error("request failed");
        if (mode === "reset-password") session.replace("");
        setResult(response.status === 202 ? "accepted" : "completed");
      }
    } catch {
      setResult("failure");
    } finally {
      form.setFieldsValue({ password: undefined, code: undefined });
      setChallenge("");
      setGeneration((value) => value + 1);
      registration.reset();
      resend.reset();
      recover.reset();
      verify.reset();
      reset.reset();
      login.reset();
    }
  }
  async function restore() {
    setResult(null);
    try {
      const r = await refresh.mutateAsync();
      if (r.status !== 200) throw new Error("request failed");
      session.replace(r.data.accessToken);
      void navigate(`/${locale}/account`);
    } catch {
      session.replace("");
      setResult("failure");
    } finally {
      refresh.reset();
    }
  }
  async function signOut() {
    try {
      const r = await logout.mutateAsync();
      if (r.status !== 204 && r.status !== 401)
        throw new Error("request failed");
      session.replace("");
    } catch {
      setResult("failure");
    } finally {
      logout.reset();
    }
  }
  const info = status.data?.status === 200 ? status.data.data : undefined;
  if (status.isPending) return <p>{copy.loading}</p>;
  if (status.isError || !info)
    return (
      <Alert
        type="error"
        title={copy.failure}
        action={
          <Button onClick={() => void status.refetch()}>{copy.submit}</Button>
        }
      />
    );
  if (!info.enabled || !info.challengeSiteKey)
    return <Alert type="info" title={copy.disabled} />;
  const emailMode = [
    "login",
    "register",
    "resend-verification",
    "recover",
  ].includes(mode ?? "");
  const passwordMode = ["login", "register", "reset-password"].includes(
    mode ?? "",
  );
  const codeMode = ["verify-email", "reset-password"].includes(mode ?? "");
  return (
    <section className="account-page">
      <h1>{titles[mode ?? "account"]}</h1>
      {result && (
        <Alert
          role="status"
          type={result === "failure" ? "error" : "success"}
          title={copy[result]}
        />
      )}
      {mode === "account" ? (
        <>
          {profile.data?.status === 200 ? (
            <>
              <p>
                {copy.signedIn}: {profile.data.data.email}
              </p>
              <Button disabled={busy} onClick={() => void signOut()}>
                {copy.logout}
              </Button>
            </>
          ) : (
            <p>{copy.sessionExpired}</p>
          )}
          <Button disabled={busy} onClick={() => void restore()}>
            {copy.restore}
          </Button>
        </>
      ) : (
        <>
          <Form
            key={mode}
            form={form}
            layout="vertical"
            onFinish={(values) => void submit(values)}
            disabled={busy}
          >
            {emailMode && (
              <Form.Item
                name="email"
                label={copy.email}
                rules={[{ required: true, type: "email" }]}
              >
                <Input type="email" autoComplete="email" maxLength={254} />
              </Form.Item>
            )}
            {passwordMode && (
              <Form.Item
                name="password"
                label={copy.password}
                rules={[
                  { required: true, min: mode === "login" ? 1 : 12, max: 1024 },
                ]}
              >
                <Input.Password
                  autoComplete={
                    mode === "login" ? "current-password" : "new-password"
                  }
                />
              </Form.Item>
            )}
            {codeMode && (
              <Form.Item
                name="code"
                label={copy.code}
                rules={[{ required: true, min: 32, max: 128 }]}
              >
                <Input autoComplete="one-time-code" />
              </Form.Item>
            )}
            <p>{copy.passwordRule}</p>
            <p>{copy.challenge}</p>
            <Challenge
              key={`${mode}-${generation}`}
              siteKey={info.challengeSiteKey}
              onToken={setChallenge}
            />
            <Button
              htmlType="submit"
              type="primary"
              loading={busy}
              disabled={!challenge || busy}
            >
              {copy.submit}
            </Button>
          </Form>
          {mode === "login" && (
            <Button disabled={busy} onClick={() => void restore()}>
              {copy.restore}
            </Button>
          )}
        </>
      )}
      <Space wrap>
        {authRoutes.map((route) => (
          <Link key={route} to={`/${locale}/${route}`}>
            {titles[route]}
          </Link>
        ))}
      </Space>
    </section>
  );
}
