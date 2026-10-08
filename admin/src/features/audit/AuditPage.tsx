import { Card, Form, Input, Table, Tag, Typography } from "antd";
import { useState } from "react";

import {
  useListAuditEvents,
  type ListAuditEventsParams,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";

export function AuditPage() {
  const auth = useAuth();
  const [params, setParams] = useState<ListAuditEventsParams>({
    limit: 20,
    offset: 0,
  });
  const query = useListAuditEvents(params, { fetch: auth.fetchOptions });
  const events = query.data?.status === 200 ? query.data.data : [];
  const pageSize = params.limit ?? 20;
  // 契约未返回总数；结果满一页时多留一页供下一页翻页。
  const total =
    (params.offset ?? 0) +
    events.length +
    (events.length >= pageSize ? pageSize : 0);
  return (
    <Card title="审计日志">
      <Form
        layout="inline"
        onFinish={(data: { action?: string; resource?: string }) =>
          setParams((current) => ({
            ...current,
            action: data.action || undefined,
            resource: data.resource || undefined,
            offset: 0,
          }))
        }
        style={{ marginBottom: 16 }}
      >
        <Form.Item name="action" label="操作">
          <Input placeholder="如 identity.user.disabled" allowClear />
        </Form.Item>
        <Form.Item name="resource" label="资源">
          <Input placeholder="如 user" allowClear />
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
