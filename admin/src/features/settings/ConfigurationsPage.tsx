import { useQueryClient } from "@tanstack/react-query";
import { Button, Card, Form, Input, message, Modal, Table, Tag } from "antd";
import { useState } from "react";
import {
  type Configuration,
  getListConfigurationsQueryKey,
  useListConfigurations,
  usePutConfiguration,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

export function ConfigurationsPage() {
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const [editing, setEditing] = useState<Configuration>();
  const [form] = Form.useForm<{ value: string }>();
  const query = useListConfigurations({ fetch: auth.fetchOptions });
  const rows = query.data?.status === 200 ? query.data.data : [];
  const put = usePutConfiguration({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("租户配置已保存");
          setEditing(undefined);
          await client.invalidateQueries({
            queryKey: getListConfigurationsQueryKey(),
          });
        } else if (response.status === 409)
          message.warning("配置已被修改，请刷新后重试");
        else if (response.status === 403) message.error("该配置不允许租户覆盖");
        else message.error("保存失败");
      },
    },
  });
  const open = (row: Configuration) => {
    setEditing(row);
    form.setFieldsValue({ value: JSON.stringify(row.value, null, 2) });
  };
  return (
    <Card title="系统配置">
      <Table
        loading={query.isLoading}
        rowKey="key"
        dataSource={rows}
        pagination={false}
        columns={[
          { title: "配置", dataIndex: "name" },
          { title: "键", dataIndex: "key" },
          { title: "类型", dataIndex: "valueType" },
          {
            title: "当前值",
            dataIndex: "value",
            render: (value) => JSON.stringify(value),
          },
          {
            title: "来源",
            dataIndex: "source",
            render: (source) => (
              <Tag>{source === "tenant" ? "租户" : "全局"}</Tag>
            ),
          },
          {
            title: "操作",
            render: (_, row) =>
              row.tenantOverridable &&
              granted.has("settings.configurations/update") ? (
                <Button type="link" onClick={() => open(row)}>
                  设置租户值
                </Button>
              ) : null,
          },
        ]}
      />
      <Modal
        title={`设置租户配置 ${editing?.key ?? ""}`}
        open={Boolean(editing)}
        onCancel={() => setEditing(undefined)}
        onOk={() => form.submit()}
        confirmLoading={put.isPending}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={({ value }) => {
            if (!editing) return;
            let decoded: unknown;
            try {
              decoded = JSON.parse(value);
            } catch {
              message.error("请输入有效 JSON 值");
              return;
            }
            put.mutate({
              configurationKey: editing.key,
              data: {
                expectedVersion:
                  editing.source === "tenant" ? editing.version : 0,
                value: decoded,
              },
            });
          }}
        >
          <Form.Item
            name="value"
            label={`JSON 值（${editing?.valueType ?? ""}）`}
            rules={[{ required: true }]}
          >
            <Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
