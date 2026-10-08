import { Card, Form, Input, Table, Tag, Typography } from "antd";
import { useState } from "react";

import {
  useListPlatformAuditEvents,
  type ListPlatformAuditEventsParams,
} from "../../api/generated/wheretolive";
import { usePlatformAuth } from "./platform-auth-context";

export function PlatformAuditPage() {
  const auth = usePlatformAuth();
  const [params, setParams] = useState<ListPlatformAuditEventsParams>({
    limit: 20,
    offset: 0,
  });
  const query = useListPlatformAuditEvents(params, {
    fetch: auth.fetchOptions,
  });
  const events = query.data?.status === 200 ? query.data.data : [];
  const pageSize = params.limit ?? 20;
  // 契约未返回总数；结果满一页时多留一页供下一页翻页。
  const total =
    (params.offset ?? 0) +
    events.length +
    (events.length >= pageSize ? pageSize : 0);
  return (
    <Card title="平台审计">
      <Form
        layout="inline"
        onFinish={(data: { tenantId?: string; action?: string }) =>
          setParams((current) => ({
            ...current,
            tenantId: data.tenantId || undefined,
            action: data.action || undefined,
            offset: 0,
          }))
        }
        style={{ marginBottom: 16 }}
      >
        <Form.Item
          name="tenantId"
          label="租户"
          rules={[
            {
              pattern:
                /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
              message: "请输入租户 UUID，留空查看全部",
            },
          ]}
        >
          <Input
            placeholder="租户 UUID（可留空）"
            allowClear
            style={{ width: 320 }}
          />
        </Form.Item>
        <Form.Item name="action" label="操作">
          <Input placeholder="如 tenant.provisioned" allowClear />
        </Form.Item>
        <Form.Item>
          <button type="submit">筛选</button>
        </Form.Item>
      </Form>
      <Table
        loading={query.isLoading}
        rowKey="id"
        dataSource={events}
        pagination={{
          pageSize,
          showSizeChanger: false,
          onChange: (page) =>
            setParams((current) => ({
              ...current,
              offset: (page - 1) * (current.limit ?? 20),
            })),
          total,
        }}
        expandable={{
          expandedRowRender: (event) => (
            <Typography.Paragraph
              style={{ maxWidth: 720, whiteSpace: "pre-wrap" }}
            >
              关联请求：{event.correlationId}
              {"\n"}变更前：
              {event.beforeState ? JSON.stringify(event.beforeState) : "无"}
              {"\n"}变更后：
              {event.afterState ? JSON.stringify(event.afterState) : "无"}
            </Typography.Paragraph>
          ),
        }}
        columns={[
          {
            title: "时间",
            dataIndex: "occurredAt",
            render: (value) => new Date(value).toLocaleString(),
          },
          {
            title: "租户",
            dataIndex: "tenantId",
            render: (value) => value ?? "平台",
          },
          { title: "操作", dataIndex: "action" },
          { title: "资源", dataIndex: "resource" },
          { title: "原因", dataIndex: "reason" },
          {
            title: "结果",
            dataIndex: "result",
            render: (result) => (
              <Tag color={result === "succeeded" ? "green" : "red"}>
                {result}
              </Tag>
            ),
          },
        ]}
      />
    </Card>
  );
}
