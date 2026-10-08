import { Button, Card, Form, Input, message, Space, Spin } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import {
  getGetMyProfileQueryKey,
  useGetMyProfile,
  useUpdateMyProfile,
  type ChangePasswordRequest,
} from "../../api/generated/wheretolive";
import { useAuth } from "./auth-context";

export function ProfilePage() {
  const auth = useAuth();
  const client = useQueryClient();
  const query = useGetMyProfile({ fetch: auth.fetchOptions });
  const profile = query.data?.status === 200 ? query.data.data : undefined;
  const update = useUpdateMyProfile({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("资料已更新");
          await client.invalidateQueries({
            queryKey: getGetMyProfileQueryKey(),
          });
        } else message.error("更新失败");
      },
    },
  });
  if (query.isLoading) return <Spin />;
  return (
    <Space direction="vertical" size="large" className="workspace">
      <Card title="个人资料">
        <Form
          key={profile?.updatedAt}
          layout="vertical"
          initialValues={profile}
          onFinish={(data) =>
            update.mutate({
              data: { username: data.username, email: data.email || null },
            })
          }
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ type: "email" }]}>
            <Input maxLength={254} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={update.isPending}>
            保存资料
          </Button>
        </Form>
      </Card>
      <Card title="修改密码">
        <Form
          layout="vertical"
          onFinish={async (
            data: ChangePasswordRequest & { confirmPassword: string },
          ) => {
            try {
              await auth.changePassword({
                currentPassword: data.currentPassword,
                newPassword: data.newPassword,
              });
              message.success("密码已修改，其他会话已退出");
            } catch {
              message.error("密码修改失败");
            }
          }}
        >
          <Form.Item
            name="currentPassword"
            label="当前密码"
            rules={[{ required: true }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            name="newPassword"
            label="新密码"
            rules={[{ required: true, min: 12 }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="confirmPassword"
            label="确认新密码"
            dependencies={["newPassword"]}
            rules={[
              { required: true },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  return !value || getFieldValue("newPassword") === value
                    ? Promise.resolve()
                    : Promise.reject(new Error("两次密码不一致"));
                },
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit">
            修改密码
          </Button>
        </Form>
      </Card>
    </Space>
  );
}
