import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Form,
  Input,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
} from "antd";
import { useState } from "react";
import {
  getListPositionsQueryKey,
  useCreatePosition,
  useListPositions,
  useUpdatePosition,
  type Position,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

export function PositionsPage() {
  const [editing, setEditing] = useState<Position>();
  const [editForm] = Form.useForm();
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const query = useListPositions({ fetch: auth.fetchOptions });
  const positions = query.data?.status === 200 ? query.data.data : [];
  const create = useCreatePosition({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 201) {
          message.success("岗位已创建");
          await client.invalidateQueries({
            queryKey: getListPositionsQueryKey(),
          });
        } else message.error("创建失败");
      },
    },
  });
  const update = useUpdatePosition({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("岗位已更新");
          setEditing(undefined);
          await client.invalidateQueries({
            queryKey: getListPositionsQueryKey(),
          });
        } else message.error("更新失败");
      },
    },
  });
  return (
    <Space direction="vertical" size="large" className="workspace">
      {granted.has("organization.positions/create") && (
        <Card title="创建岗位">
          <Form layout="inline" onFinish={(data) => create.mutate({ data })}>
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input maxLength={128} />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={create.isPending}>
              创建
            </Button>
          </Form>
        </Card>
      )}
      <Card title="岗位">
        <Table
          rowKey="id"
          loading={query.isLoading}
          dataSource={positions}
          pagination={false}
          columns={[
            { title: "名称", dataIndex: "name" },
            {
              title: "状态",
              render: (_, item) => (
                <Tag color={item.status === "active" ? "green" : "default"}>
                  {item.status === "active" ? "启用" : "停用"}
                </Tag>
              ),
            },
            {
              title: "操作",
              render: (_, item) =>
                granted.has("organization.positions/update") ? (
                  <Button
                    onClick={() => {
                      setEditing(item);
                      editForm.setFieldsValue({
                        name: item.name,
                        status: item.status,
                      });
                    }}
                  >
                    编辑
                  </Button>
                ) : null,
            },
          ]}
        />
      </Card>
      <Modal
        title="编辑岗位"
        open={Boolean(editing)}
        confirmLoading={update.isPending}
        onCancel={() => setEditing(undefined)}
        onOk={() => editForm.submit()}
        destroyOnHidden
      >
        <Form
          form={editForm}
          layout="vertical"
          onFinish={(data) =>
            editing && update.mutate({ positionId: editing.id, data })
          }
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select
              options={[
                { value: "active", label: "启用" },
                { value: "disabled", label: "停用" },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
