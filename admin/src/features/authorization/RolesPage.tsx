import { useQueryClient } from "@tanstack/react-query";
import { Button, Card, Form, Input, message, Space, Table, Tag } from "antd";
import {
  getListRolesQueryKey,
  useCreateRole,
  useListRoles,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";
import { useNavigate } from "react-router-dom";

export function RolesPage() {
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const navigate = useNavigate();
  const query = useListRoles({ fetch: auth.fetchOptions });
  const roles = query.data?.status === 200 ? query.data.data : [];
  const create = useCreateRole({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 201) {
          message.success("角色已创建");
          await client.invalidateQueries({ queryKey: getListRolesQueryKey() });
        } else message.error("创建失败");
      },
    },
  });
  return (
    <Space direction="vertical" size="large" className="workspace">
      {granted.has("authorization.roles/create") && (
        <Card title="创建角色">
          <Form layout="inline" onFinish={(data) => create.mutate({ data })}>
            <Form.Item
              name="code"
              label="编码"
              rules={[
                {
                  required: true,
                  pattern: /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/,
                },
              ]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={create.isPending}>
              创建
            </Button>
          </Form>
        </Card>
      )}
      <Card title="角色与策略">
        <Table
          rowKey="id"
          loading={query.isLoading}
          dataSource={roles}
          pagination={false}
          columns={[
            { title: "名称", dataIndex: "name" },
            { title: "编码", dataIndex: "code" },
            {
              title: "类型",
              render: (_, role) =>
                role.reserved ? (
                  <Tag color="blue">系统保留</Tag>
                ) : (
                  <Tag>自定义</Tag>
                ),
            },
            { title: "版本", dataIndex: "version" },
            {
              title: "操作",
              render: (_, role) =>
                granted.has("authorization.policies/read") ? (
                  <Button
                    onClick={() =>
                      void navigate(`/authorization/roles/${role.id}/policies`)
                    }
                  >
                    策略
                  </Button>
                ) : null,
            },
          ]}
        />
      </Card>
    </Space>
  );
}
