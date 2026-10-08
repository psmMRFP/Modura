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
  Popconfirm,
  Space,
  Tag,
} from "antd";
import { useState } from "react";
import {
  getListDictionariesQueryKey,
  type Dictionary,
  type ReplaceDictionaryRequest,
  useDeleteDictionary,
  useListDictionaries,
  useReplaceDictionary,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

type DictionaryForm = ReplaceDictionaryRequest & { code: string };

export function DictionariesPage() {
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const [editing, setEditing] = useState<Dictionary | null>();
  const [form] = Form.useForm<DictionaryForm>();
  const query = useListDictionaries({ fetch: auth.fetchOptions });
  const rows = query.data?.status === 200 ? query.data.data : [];
  const writeFetch = {
    ...auth.fetchOptions,
    headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
  };
  const refresh = () =>
    client.invalidateQueries({ queryKey: getListDictionariesQueryKey() });
  const replace = useReplaceDictionary({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("字典已保存");
          setEditing(undefined);
          form.resetFields();
          await refresh();
        } else if (response.status === 409)
          message.warning("字典已被修改，请刷新后重试");
        else message.error("保存失败");
      },
    },
  });
  const remove = useDeleteDictionary({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("租户字典已删除");
          await refresh();
        } else if (response.status === 409)
          message.warning("版本已变更，请刷新后重试");
        else message.error("删除失败");
      },
    },
  });
  const open = (row?: Dictionary) => {
    setEditing(row ?? null);
    form.setFieldsValue({
      code: row?.code ?? "",
      name: row?.name ?? "",
      expectedVersion: row?.source === "tenant" ? row.version : 0,
      items: row?.items ?? [],
    });
  };
  return (
    <Card
      loading={query.isLoading}
      title="字典管理"
      extra={
        granted.has("settings.dictionaries/update") ? (
          <Button type="primary" onClick={() => open()}>
            新建租户字典
          </Button>
        ) : null
      }
    >
      <List
        dataSource={rows}
        locale={{ emptyText: "暂无有效字典" }}
        renderItem={(row) => (
          <List.Item
            actions={[
              granted.has("settings.dictionaries/update") ? (
                <Button key="edit" type="link" onClick={() => open(row)}>
                  {row.source === "global" ? "创建租户覆盖" : "编辑"}
                </Button>
              ) : null,
              row.source === "tenant" &&
              granted.has("settings.dictionaries/delete") ? (
                <Popconfirm
                  key="delete"
                  title="删除租户字典？"
                  onConfirm={() =>
                    remove.mutate({
                      dictionaryCode: row.code,
                      params: { expectedVersion: row.version },
                    })
                  }
                >
                  <Button danger type="link">
                    删除
                  </Button>
                </Popconfirm>
              ) : null,
            ].filter(Boolean)}
          >
            <List.Item.Meta
              title={
                <Space>
                  {row.name}
                  <Tag>{row.code}</Tag>
                  <Tag color={row.source === "tenant" ? "blue" : "default"}>
                    {row.source === "tenant" ? "租户" : "全局"}
                  </Tag>
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
        title={editing ? `编辑 ${editing.code}` : "新建租户字典"}
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
          <Form.Item name="expectedVersion" hidden>
            <InputNumber />
          </Form.Item>
          <Form.List name="items">
            {(fields, { add, remove: removeItem }) => (
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
                    <Button danger onClick={() => removeItem(field.name)}>
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
