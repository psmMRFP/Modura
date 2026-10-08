import { Button, Layout, Menu, Spin, Typography } from "antd";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";
import { usePlatformAuth } from "./platform-auth-context";
export function PlatformLayout() {
  const auth = usePlatformAuth();
  const location = useLocation();
  const navigate = useNavigate();
  if (auth.status === "loading") return <Spin fullscreen />;
  if (auth.status === "anonymous")
    return (
      <Navigate
        to="/platform/login"
        state={{ from: location.pathname }}
        replace
      />
    );
  return (
    <Layout className="admin-shell">
      <Layout.Sider breakpoint="lg" collapsedWidth="0">
        <div className="admin-shell__brand">WhereToLive Operations</div>
        <Menu
          theme="dark"
          selectedKeys={[location.pathname]}
          onClick={({ key }) => void navigate(key)}
          items={[
            { key: "/platform", label: "租户管理" },
            { key: "/platform/audit", label: "平台审计" },
            { key: "/platform/places", label: "地点目录" },
            { key: "/platform/settings/dictionaries", label: "全局字典" },
            { key: "/platform/settings/configurations", label: "全局配置" },
          ]}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="admin-shell__header">
          <Typography.Text strong>平台控制台</Typography.Text>
          <Button onClick={() => void auth.logout()}>退出登录</Button>
        </Layout.Header>
        <Layout.Content className="admin-shell__content">
          <Outlet />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
