import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  InputNumber,
  List,
  message,
  Modal,
  Space,
  Tag,
} from "antd";
import { useState } from "react";
import {
  getListPlatformDictionariesQueryKey,
  type Dictionary,
  type ReplacePlatformDictionaryRequest,
  useListPlatformDictionaries,
  useReplacePlatformDictionary,
} from "../../api/generated/wheretolive";
import { usePlatformAuth } from "./platform-auth-context";

type DictionaryForm = ReplacePlatformDictionaryRequest & { code: string };

export function PlatformDictionariesPage() {
  const auth = usePlatformAuth();
  const client = useQueryClient();
  const [editing, setEditing] = useState<Dictionary | null>();
  const [form] = Form.useForm<DictionaryForm>();
  const query = useListPlatformDictionaries({ fetch: auth.fetchOptions });
  const rows = query.data?.status === 200 ? query.data.data : [];
  const replace = useReplacePlatformDictionary({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("全局字典已保存");
          setEditing(undefined);
          form.resetFields();
          await client.invalidateQueries({
            queryKey: getListPlatformDictionariesQueryKey(),
          });
        } else if (response.status === 409)
          message.warning("字典已被修改，请刷新后重试");
        else message.error("保存失败");
      },
    },
  });
  const open = (row?: Dictionary) => {
    setEditing(row ?? null);
    form.setFieldsValue({
      code: row?.code ?? "",
      name: row?.name ?? "",
      expectedVersion: row?.version ?? 0,
      items: row?.items ?? [],
      reason: "",
    });
  };
  return (
    <Card
      title="全局字典"
      loading={query.isLoading}
      extra={
        <Button type="primary" onClick={() => open()}>
          新建全局字典
        </Button>
      }
    >
      <List
        dataSource={rows}
        locale={{ emptyText: "暂无全局字典" }}
        renderItem={(row) => (
          <List.Item
            actions={[
              <Button key="edit" type="link" onClick={() => open(row)}>
                编辑
              </Button>,
            ]}
          >
            <List.Item.Meta
              title={
                <Space>
                  {row.name}
                  <Tag>{row.code}</Tag>
                  <Tag>v{row.version}</Tag>
                </Space>
              }
              description={row.items
                .map((item) => `${item.label} (${item.code})`)
                .join(" · ")}
            />
          </List.Item>
        )}
      />
      <Modal
        title={editing ? `编辑 ${editing.code}` : "新建全局字典"}
        open={editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={() => form.submit()}
        confirmLoading={replace.isPending}
        width={760}
      >
        <Form<DictionaryForm>
          form={form}
          layout="vertical"
          onFinish={({ code, ...data }) =>
            replace.mutate({ dictionaryCode: code, data })
          }
        >
          <Form.Item name="code" label="编码" rules={[{ required: true }]}>
            <Input disabled={Boolean(editing)} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="reason"
            label="审计原因"
            rules={[{ required: true }]}
          >
            <Input.TextArea maxLength={500} />
          </Form.Item>
          <Form.Item name="expectedVersion" hidden>
            <InputNumber />
          </Form.Item>
          <Form.List name="items">
            {(fields, { add, remove }) => (
              <Space direction="vertical" className="workspace">
                {fields.map((field) => (
                  <Space key={field.key} align="baseline" wrap>
                    <Form.Item
                      {...field}
                      name={[field.name, "code"]}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="项编码" />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, "label"]}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="显示值" />
                    </Form.Item>
                    <Form.Item {...field} name={[field.name, "sortOrder"]}>
                      <InputNumber placeholder="排序" />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, "enabled"]}
                      valuePropName="checked"
                    >
                      <Checkbox>启用</Checkbox>
                    </Form.Item>
                    <Button danger onClick={() => remove(field.name)}>
                      移除
                    </Button>
                  </Space>
                ))}
                <Button onClick={() => add({ sortOrder: 0, enabled: true })}>
                  添加字典项
                </Button>
              </Space>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Card>
  );
}
