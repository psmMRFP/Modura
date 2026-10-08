import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";

import type { LoginRequest } from "../../api/generated/wheretolive";
import { useAuth } from "./auth-context";

export function LoginPage() {
  const auth = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState("");

  if (auth.status === "authenticated") {
    return <Navigate to="/" replace />;
  }

  const submit = async (values: LoginRequest) => {
    setError("");
    try {
      await auth.login(values);
      const destination = (location.state as { from?: string } | null)?.from;
      await navigate(destination || "/", { replace: true });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "登录失败");
    }
  };

  return (
    <main className="login-page">
      <Card className="login-card">
        <Typography.Title level={2}>登录 WhereToLive</Typography.Title>
        <Typography.Paragraph type="secondary">
          使用租户标识和本地账号进入管理工作台。
        </Typography.Paragraph>
        {error ? <Alert type="error" message={error} showIcon /> : null}
        <Form<LoginRequest>
          layout="vertical"
          onFinish={(values) => void submit(values)}
        >
          <Form.Item name="tenant" label="租户" rules={[{ required: true }]}>
            <Input autoComplete="organization" />
          </Form.Item>
          <Form.Item
            name="login"
            label="账号或邮箱"
            rules={[{ required: true }]}
          >
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            登录
          </Button>
        </Form>
      </Card>
    </main>
  );
}
