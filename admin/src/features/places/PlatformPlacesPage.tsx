import { useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  InputNumber,
  List,
  message,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  getListPlatformPlacesQueryKey,
  type CreatePlatformPlaceRequest,
  type ManagedPlace,
  useCreatePlatformPlace,
  useListPlatformPlaces,
  useSetPlacePublication,
  useUpdatePlatformPlace,
} from "../../api/generated/modura";
import { usePlatformAuth } from "../platform/platform-auth-context";
import { emptyPlaceDetails, normalizePlaceDetails } from "./form";

const kinds = [
  { value: "country", label: "国家" },
  { value: "region", label: "地区 / 省州" },
  { value: "island", label: "岛屿" },
  { value: "city", label: "城市" },
  { value: "district", label: "城区" },
];

export function PlatformPlacesPage() {
  const auth = usePlatformAuth();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const rawOffset = params.get("offset") ?? "0";
  const offset =
    /^\d+$/.test(rawOffset) && Number(rawOffset) <= 10000
      ? Number(rawOffset)
      : 0;
  const [editing, setEditing] = useState<ManagedPlace | null>();
  const [publication, setPublication] = useState<ManagedPlace>();
  const [form] = Form.useForm<CreatePlatformPlaceRequest>();
  const [publishForm] = Form.useForm<{ reason: string }>();
  const kind = Form.useWatch("type", form) ?? "country";
  const query = useListPlatformPlaces(
    { q, offset, limit: 20 },
    { fetch: auth.fetchOptions, query: { retry: false } },
  );
  const page = query.data?.status === 200 ? query.data.data : undefined;
  const fetchOptions = {
    ...auth.fetchOptions,
    headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
  };
  const saved = async (response: { status: number }) => {
    if (response.status === 200 || response.status === 201) {
      message.success("地点变更已保存并记录审计");
      setEditing(undefined);
      setPublication(undefined);
      form.resetFields();
      publishForm.resetFields();
    } else if (response.status === 409)
      message.warning(
        "地点版本、唯一标识或上级发布状态冲突；请刷新列表，重新打开后再操作。",
      );
    else message.error("保存失败，请检查输入或平台会话。");
    await client.invalidateQueries({
      queryKey: getListPlatformPlacesQueryKey(),
    });
  };
  const mutation = {
    onSuccess: saved,
    onError: () => message.error("请求失败，请稍后重试。"),
  };
  const create = useCreatePlatformPlace({ fetch: fetchOptions, mutation });
  const update = useUpdatePlatformPlace({ fetch: fetchOptions, mutation });
  const publish = useSetPlacePublication({ fetch: fetchOptions, mutation });
  const busy = create.isPending || update.isPending || publish.isPending;
  const open = (entry?: ManagedPlace) => {
    form.resetFields();
    form.setFieldsValue({
      slug: entry?.slug ?? "",
      type: entry?.type ?? "country",
      parentId: entry?.parentId ?? null,
      countryCode: entry?.countryCode ?? "",
      details: entry?.details ?? { ...emptyPlaceDetails },
      reason: "",
    });
    setEditing(entry ?? null);
  };
  const search = (value: string) => {
    const next = new URLSearchParams();
    if (value) next.set("q", value);
    setParams(next);
  };
  const go = (value: number) => {
    const next = new URLSearchParams(params);
    if (value) next.set("offset", String(value));
    else next.delete("offset");
    setParams(next);
  };
  return (
    <Card
      title="地点目录"
      extra={
        <Button type="primary" disabled={busy} onClick={() => open()}>
          新建地点
        </Button>
      }
    >
      <Space orientation="vertical" style={{ width: "100%" }} size="large">
        <Alert
          type="info"
          showIcon
          title="新地点先保存为草稿；发布需上级已发布。撤回上级后，其下级在公开站点也会隐藏。地点标识和地理归属创建后固定。"
        />
        <Input.Search
          key={q}
          defaultValue={q}
          maxLength={120}
          placeholder="按名称或 slug 搜索"
          onSearch={search}
          enterButton="搜索"
        />
        {query.isError || (query.data && query.data.status !== 200) ? (
          <Alert
            type="error"
            title="地点列表加载失败"
            action={<Button onClick={() => void query.refetch()}>重试</Button>}
          />
        ) : (
          <List
            loading={query.isLoading}
            dataSource={page?.items ?? []}
            locale={{ emptyText: "暂无地点" }}
            renderItem={(entry) => (
              <List.Item
                actions={[
                  <Button
                    key="edit"
                    disabled={busy}
                    onClick={() => open(entry)}
                  >
                    编辑
                  </Button>,
                  <Button
                    key="publish"
                    disabled={busy}
                    danger={entry.publishedAt !== null}
                    onClick={() => {
                      publishForm.resetFields();
                      setPublication(entry);
                    }}
                  >
                    {entry.publishedAt ? "撤回" : "发布"}
                  </Button>,
                ]}
              >
                <List.Item.Meta
                  title={
                    <Space>
                      <Typography.Text strong>
                        {entry.details.name}
                      </Typography.Text>
                      <Tag>{entry.type}</Tag>
                      <Tag>{entry.countryCode}</Tag>
                      <Tag color={entry.publishedAt ? "green" : "default"}>
                        {entry.publishedAt ? "已标记发布" : "草稿"}
                      </Tag>
                      <Tag>v{entry.version}</Tag>
                    </Space>
                  }
                  description={
                    <>
                      <div>{entry.slug}</div>
                      <Typography.Text copyable>{entry.id}</Typography.Text>
                    </>
                  }
                />
              </List.Item>
            )}
          />
        )}
        <Space>
          <Button
            disabled={offset === 0}
            onClick={() => go(Math.max(0, offset - 20))}
          >
            上一页
          </Button>
          <Button
            disabled={page?.nextOffset == null || page.nextOffset > 10000}
            onClick={() => go(page?.nextOffset ?? 0)}
          >
            下一页
          </Button>
          <Button onClick={() => void query.refetch()}>刷新</Button>
        </Space>
      </Space>
      <Modal
        title={editing ? `编辑 ${editing.details.name}` : "新建地点草稿"}
        open={editing !== undefined}
        onCancel={() => {
          if (!busy) setEditing(undefined);
        }}
        onOk={() => form.submit()}
        confirmLoading={busy}
        width={800}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(values) => {
            const details = normalizePlaceDetails(values.details);
            if (editing) {
              update.mutate({
                placeId: editing.id,
                data: {
                  expectedVersion: editing.version,
                  details,
                  reason: values.reason,
                },
              });
            } else {
              create.mutate({
                data: {
                  ...values,
                  parentId:
                    values.type === "country"
                      ? null
                      : values.parentId?.trim() || null,
                  countryCode: values.countryCode.trim().toUpperCase(),
                  details,
                },
              });
            }
          }}
        >
          <Form.Item
            name="slug"
            label="稳定 URL 标识"
            rules={[
              { required: true },
              {
                pattern: /^[a-z0-9]+(-[a-z0-9]+)*$/,
                message: "使用小写英文、数字及连接符",
              },
            ]}
          >
            <Input maxLength={120} disabled={Boolean(editing)} />
          </Form.Item>
          <Form.Item name="type" label="地点类型" rules={[{ required: true }]}>
            <Select options={kinds} disabled={Boolean(editing)} />
          </Form.Item>
          <Form.Item
            name="countryCode"
            label="ISO 两位国家代码"
            rules={[
              { required: true },
              { pattern: /^[A-Za-z]{2}$/, message: "请输入两位国家代码" },
            ]}
          >
            <Input maxLength={2} placeholder="DE" disabled={Boolean(editing)} />
          </Form.Item>
          <Form.Item
            name="parentId"
            label="上级地点 ID"
            rules={[{ required: kind !== "country" }]}
            extra="从地点列表复制上级 ID；国家无需上级。"
          >
            <Input disabled={Boolean(editing) || kind === "country"} />
          </Form.Item>
          <Form.Item
            name={["details", "name"]}
            label="原始名称"
            rules={[{ required: true }]}
          >
            <Input maxLength={200} />
          </Form.Item>
          <Space wrap>
            <Form.Item name={["details", "timezone"]} label="IANA 时区">
              <Input placeholder="Europe/Berlin" />
            </Form.Item>
            <Form.Item name={["details", "currency"]} label="ISO 货币代码">
              <Input maxLength={3} placeholder="EUR" />
            </Form.Item>
          </Space>
          <Space wrap>
            <Form.Item name={["details", "latitude"]} label="纬度">
              <InputNumber min={-90} max={90} />
            </Form.Item>
            <Form.Item name={["details", "longitude"]} label="经度">
              <InputNumber min={-180} max={180} />
            </Form.Item>
          </Space>
          <Form.Item name={["details", "languages"]} label="当地语言（BCP-47）">
            <Select
              mode="tags"
              tokenSeparators={[","]}
              options={["en", "zh-CN", "de", "fr", "es"].map((value) => ({
                value,
                label: value,
              }))}
            />
          </Form.Item>
          <Typography.Title level={5}>多语言名称与别名</Typography.Title>
          <Form.List name={["details", "aliases"]}>
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name, ...rest }) => (
                  <Space key={key} align="baseline" wrap>
                    <Form.Item
                      {...rest}
                      name={[name, "locale"]}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="语言代码，如 zh-CN" maxLength={35} />
                    </Form.Item>
                    <Form.Item
                      {...rest}
                      name={[name, "name"]}
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="地点名称或别名" maxLength={200} />
                    </Form.Item>
                    <Form.Item
                      {...rest}
                      name={[name, "preferred"]}
                      valuePropName="checked"
                    >
                      <Checkbox>该语言首选名</Checkbox>
                    </Form.Item>
                    <Button onClick={() => remove(name)}>移除</Button>
                  </Space>
                ))}
                <Button
                  onClick={() =>
                    add({ locale: "en", name: "", preferred: false })
                  }
                >
                  添加别名
                </Button>
              </>
            )}
          </Form.List>
          <Form.Item
            name="reason"
            label="变更原因（审计）"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input.TextArea maxLength={500} rows={2} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={publication?.publishedAt ? "撤回地点" : "发布地点"}
        open={publication !== undefined}
        onCancel={() => {
          if (!busy) setPublication(undefined);
        }}
        onOk={() => publishForm.submit()}
        confirmLoading={publish.isPending}
      >
        <Typography.Paragraph>
          {publication?.publishedAt
            ? "撤回后该地点及其下级不再对公众显示；下级的发布标记保留，上级重新发布后可恢复可见。"
            : "发布基础地点信息，不生成任何签证、税务、成本数据或评分。上级地点必须已经发布。"}
        </Typography.Paragraph>
        <Form
          form={publishForm}
          layout="vertical"
          onFinish={({ reason }) => {
            if (publication)
              publish.mutate({
                placeId: publication.id,
                data: {
                  expectedVersion: publication.version,
                  published: publication.publishedAt === null,
                  reason,
                },
              });
          }}
        >
          <Form.Item
            name="reason"
            label="操作原因（审计）"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input.TextArea maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
