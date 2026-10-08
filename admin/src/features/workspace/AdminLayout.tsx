import { Button, Layout, Menu, Result, Spin, Typography } from "antd";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";

import { useListEffectivePermissions } from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { visibleNavigation } from "./navigation";

export function AdminLayout() {
  const auth = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const permissions = useListEffectivePermissions({
    query: { enabled: auth.status === "authenticated" },
    fetch: auth.fetchOptions,
  });

  if (auth.status === "loading") {
    return <Spin fullscreen />;
  }
  if (auth.status === "anonymous") {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  if (permissions.isLoading) {
    return <Spin fullscreen />;
  }
  if (!permissions.data || permissions.data.status !== 200) {
    return <Result status="403" title="无法读取当前权限" />;
  }

  const granted = new Set(
    permissions.data.data.map((item) => `${item.resource}/${item.action}`),
  );
  const items = visibleNavigation(granted);

  return (
    <Layout className="admin-shell">
      <Layout.Sider breakpoint="lg" collapsedWidth="0">
        <div className="admin-shell__brand">WhereToLive</div>
        <Menu
          theme="dark"
          selectedKeys={[location.pathname]}
          items={items}
          onClick={({ key }) => void navigate(key)}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="admin-shell__header">
          <Typography.Text strong>AI 加持的快速开发平台</Typography.Text>
          <Button onClick={() => void auth.logout()}>退出登录</Button>
        </Layout.Header>
        <Layout.Content className="admin-shell__content">
          <Outlet context={{ granted }} />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
