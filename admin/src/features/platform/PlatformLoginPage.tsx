import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import type { PlatformLoginRequest } from "../../api/generated/wheretolive";
import { usePlatformAuth } from "./platform-auth-context";

export function PlatformLoginPage() {
  const auth = usePlatformAuth();
  const navigate = useNavigate();
  const [error, setError] = useState("");
  if (auth.status === "authenticated")
    return <Navigate to="/platform" replace />;
  return (
    <main className="login-page">
      <Card className="login-card">
        <Typography.Title level={2}>平台管理登录</Typography.Title>
        <Typography.Paragraph type="secondary">
          此入口使用独立的平台管理员身份，不接受租户访问令牌。
        </Typography.Paragraph>
        {error && <Alert type="error" message={error} showIcon />}
        <Form<PlatformLoginRequest>
          layout="vertical"
          onFinish={async (data) => {
            try {
              await auth.login(data);
              await navigate("/platform", { replace: true });
            } catch {
              setError("登录失败，请检查账号和密码");
            }
          }}
        >
          <Form.Item
            name="username"
            label="平台账号"
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
