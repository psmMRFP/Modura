import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  message,
  Modal,
  Select,
  Table,
  Tag,
} from "antd";
import { useState } from "react";
import {
  ConfigurationValueType,
  type Configuration,
  getListPlatformConfigurationsQueryKey,
  useListPlatformConfigurations,
  usePutPlatformConfiguration,
} from "../../api/generated/wheretolive";
import { usePlatformAuth } from "./platform-auth-context";

type ConfigurationForm = {
  key: string;
  name: string;
  valueType: ConfigurationValueType;
  tenantOverridable: boolean;
  expectedVersion: number;
  value: string;
  reason: string;
};

export function PlatformConfigurationsPage() {
  const auth = usePlatformAuth();
  const client = useQueryClient();
  const [editing, setEditing] = useState<Configuration | null>();
  const [form] = Form.useForm<ConfigurationForm>();
  const query = useListPlatformConfigurations({ fetch: auth.fetchOptions });
  const rows = query.data?.status === 200 ? query.data.data : [];
  const put = usePutPlatformConfiguration({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("全局配置已保存");
          setEditing(undefined);
          form.resetFields();
          await client.invalidateQueries({
            queryKey: getListPlatformConfigurationsQueryKey(),
          });
        } else if (response.status === 409)
          message.warning("配置已被修改，请刷新后重试");
        else message.error("保存失败");
      },
    },
  });
  const open = (row?: Configuration) => {
    setEditing(row ?? null);
    form.setFieldsValue({
      key: row?.key ?? "",
      name: row?.name ?? "",
      valueType: row?.valueType ?? ConfigurationValueType.string,
      tenantOverridable: row?.tenantOverridable ?? false,
      expectedVersion: row?.version ?? 0,
      value: row ? JSON.stringify(row.value, null, 2) : '""',
      reason: "",
    });
  };
  return (
    <Card
      title="全局非密配置"
      extra={
        <Button type="primary" onClick={() => open()}>
          新建全局配置
        </Button>
      }
    >
      <Table
        rowKey="key"
        loading={query.isLoading}
        dataSource={rows}
        pagination={false}
        columns={[
          { title: "名称", dataIndex: "name" },
          { title: "键", dataIndex: "key" },
          { title: "类型", dataIndex: "valueType" },
          { title: "值", render: (_, row) => JSON.stringify(row.value) },
          {
            title: "租户可覆盖",
            render: (_, row) => (
              <Tag color={row.tenantOverridable ? "blue" : "default"}>
                {row.tenantOverridable ? "是" : "否"}
              </Tag>
            ),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Button type="link" onClick={() => open(row)}>
                编辑
              </Button>
            ),
          },
        ]}
      />
      <Modal
        title={editing ? `编辑 ${editing.key}` : "新建全局配置"}
        open={editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={() => form.submit()}
        confirmLoading={put.isPending}
      >
        <Form<ConfigurationForm>
          form={form}
          layout="vertical"
          onFinish={(value) => {
            let decoded: unknown;
            try {
              decoded = JSON.parse(value.value);
            } catch {
              message.error("请输入有效 JSON 值");
              return;
            }
            put.mutate({
              configurationKey: value.key,
              data: {
                name: value.name,
                valueType: value.valueType,
                tenantOverridable: value.tenantOverridable,
                expectedVersion: value.expectedVersion,
                value: decoded,
                reason: value.reason,
              },
            });
          }}
        >
          <Form.Item name="key" label="键" rules={[{ required: true }]}>
            <Input disabled={Boolean(editing)} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="valueType"
            label="值类型"
            rules={[{ required: true }]}
          >
            <Select
              disabled={Boolean(editing)}
              options={Object.values(ConfigurationValueType).map((value) => ({
                value,
                label: value,
              }))}
            />
          </Form.Item>
          <Form.Item name="tenantOverridable" valuePropName="checked">
            <Checkbox>允许租户覆盖</Checkbox>
          </Form.Item>
          <Form.Item name="value" label="JSON 值" rules={[{ required: true }]}>
            <Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} />
          </Form.Item>
          <Form.Item
            name="reason"
            label="审计原因"
            rules={[{ required: true }]}
          >
            <Input.TextArea maxLength={500} />
          </Form.Item>
          <Form.Item name="expectedVersion" hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
