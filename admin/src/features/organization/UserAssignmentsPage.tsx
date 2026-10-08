import {
  Button,
  Card,
  Form,
  Input,
  message,
  Modal,
  Select,
  Space,
  Typography,
} from "antd";
import { useState } from "react";
import {
  useAssignUserOrganization,
  useDisableTenantUser,
  useGetUserRoleGrants,
  useGetTenantUser,
  useListDepartments,
  useListPositions,
  useListRoles,
  useListTenantUsers,
  useReplaceUserRoleGrants,
  useUnlockTenantUser,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

export function UserAssignmentsPage() {
  const [userId, setUserId] = useState("");
  const auth = useAuth();
  const granted = usePermissions();
  const departmentsQuery = useListDepartments({ fetch: auth.fetchOptions });
  const positionsQuery = useListPositions({ fetch: auth.fetchOptions });
  const rolesQuery = useListRoles({ fetch: auth.fetchOptions });
  const usersQuery = useListTenantUsers({ fetch: auth.fetchOptions });
  const userQuery = useGetTenantUser(userId, {
    fetch: auth.fetchOptions,
    query: { enabled: Boolean(userId) },
  });
  const grantsQuery = useGetUserRoleGrants(userId, {
    fetch: auth.fetchOptions,
    query: { enabled: Boolean(userId) },
  });
  const departments =
    departmentsQuery.data?.status === 200 ? departmentsQuery.data.data : [];
  const positions =
    positionsQuery.data?.status === 200 ? positionsQuery.data.data : [];
  const roles =
    rolesQuery.data?.status === 200
      ? rolesQuery.data.data.filter((role) => !role.reserved)
      : [];
  const grants =
    grantsQuery.data?.status === 200 ? grantsQuery.data.data : undefined;
  const users = usersQuery.data?.status === 200 ? usersQuery.data.data : [];
  const user = userQuery.data?.status === 200 ? userQuery.data.data : undefined;
  const writeFetch = {
    ...auth.fetchOptions,
    headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
  };
  const assign = useAssignUserOrganization({
    fetch: writeFetch,
    mutation: {
      onSuccess: (response) =>
        response.status === 204
          ? message.success("组织归属已更新")
          : message.error("更新失败"),
    },
  });
  const replace = useReplaceUserRoleGrants({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("角色授权已更新");
          await grantsQuery.refetch();
        } else if (response.status === 409)
          message.warning("授权已被其他管理员修改，请刷新后重试");
        else message.error("更新失败");
      },
    },
  });
  const refreshUser = async () => {
    await Promise.all([usersQuery.refetch(), userQuery.refetch()]);
  };
  const disable = useDisableTenantUser({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("账户已停用");
          await refreshUser();
        } else if (response.status === 404)
          message.warning("用户不存在或不属于当前租户");
        else message.error("停用失败");
      },
    },
  });
  const unlock = useUnlockTenantUser({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("账户已解锁");
          await refreshUser();
        } else if (response.status === 404)
          message.warning("用户不存在或不属于当前租户");
        else message.error("解锁失败");
      },
    },
  });
  const confirmUnlock = () => {
    Modal.confirm({
      title: "解锁账户",
      content: "确认将该锁定账户恢复为可用状态？",
      okText: "解锁",
      cancelText: "取消",
      onOk: () => unlock.mutateAsync({ userId }),
    });
  };
  return (
    <Space direction="vertical" size="large" className="workspace">
      <Card title="用户目录">
        <Typography.Paragraph type="secondary">
          选择当前租户中的用户，查看账户详情及授权。
        </Typography.Paragraph>
        <Select
          showSearch
          optionFilterProp="label"
          placeholder="选择用户"
          style={{ width: 340 }}
          loading={usersQuery.isLoading}
          options={users.map((item) => ({
            value: item.id,
            label: `${item.username} · ${item.status}`,
          }))}
          value={userId || undefined}
          onChange={setUserId}
        />
        {user && (
          <Typography.Paragraph>
            用户：{user.username} · 状态：{user.status} · 邮箱：
            {user.email ?? "未设置"}
          </Typography.Paragraph>
        )}
      </Card>
      {userId && granted.has("identity.users/update") && (
        <Card title="账户状态">
          {user?.status === "disabled" ? (
            <Typography.Paragraph type="warning">
              该账户已停用，无法登录；停用状态不能在页面内恢复。
            </Typography.Paragraph>
          ) : (
            <Form
              layout="inline"
              onFinish={(data: { reason: string }) =>
                disable.mutate({ userId, data: { reason: data.reason } })
              }
            >
              <Form.Item
                name="reason"
                label="停用原因"
                rules={[
                  { required: true, message: "请填写停用原因" },
                  { max: 256 },
                ]}
              >
                <Input
                  placeholder="写入审计记录的原因"
                  style={{ width: 280 }}
                />
              </Form.Item>
              <Space>
                <Button danger htmlType="submit" loading={disable.isPending}>
                  停用账户
                </Button>
                {user?.status === "locked" && (
                  <Button loading={unlock.isPending} onClick={confirmUnlock}>
                    解锁账户
                  </Button>
                )}
              </Space>
            </Form>
          )}
        </Card>
      )}
      {userId && granted.has("organization.user-organization/update") && (
        <Card title="组织归属">
          <Form
            layout="inline"
            onFinish={(data) => assign.mutate({ userId, data })}
          >
            <Form.Item
              name="departmentId"
              label="主部门"
              rules={[{ required: true }]}
            >
              <Select
                style={{ width: 220 }}
                options={departments.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
              />
            </Form.Item>
            <Form.Item name="positionId" label="岗位">
              <Select
                allowClear
                style={{ width: 220 }}
                options={positions.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={assign.isPending}>
              保存
            </Button>
          </Form>
        </Card>
      )}
      {userId && grants && granted.has("authorization.user-roles/update") && (
        <Card title="角色授权">
          <Form
            key={grants.version}
            initialValues={{ roleIds: grants.roleIds }}
            onFinish={(data: { roleIds: string[] }) =>
              replace.mutate({
                userId,
                data: {
                  expectedVersion: grants.version,
                  roleIds: data.roleIds ?? [],
                },
              })
            }
          >
            <Form.Item name="roleIds" label={`角色（版本 ${grants.version}）`}>
              <Select
                mode="multiple"
                options={roles.map((role) => ({
                  value: role.id,
                  label: role.name,
                }))}
              />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              loading={replace.isPending}
            >
              保存期望状态
            </Button>
          </Form>
        </Card>
      )}
    </Space>
  );
}
