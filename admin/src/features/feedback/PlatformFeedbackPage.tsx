import { useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
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
  FeedbackStatus,
  getListPlatformFeedbackQueryKey,
  type CreateFeedbackRequest,
  type FeedbackEntry,
  type ReviewFeedbackRequest,
  useCreatePlatformFeedback,
  useListPlatformFeedback,
  useListPlatformFeedbackCategories,
  useListPlatformPlaces,
  useReviewPlatformFeedback,
} from "../../api/generated/wheretolive";
import { usePlatformAuth } from "../platform/platform-auth-context";

const labels: Record<FeedbackStatus, string> = {
  open: "待处理",
  in_review: "审核中",
  resolved: "已解决",
  dismissed: "不处理",
};

export function PlatformFeedbackPage() {
  const auth = usePlatformAuth();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const rawStatus = params.get("status");
  const status = Object.values(FeedbackStatus).find(
    (value) => value === rawStatus,
  );
  const category = params.get("category") || undefined;
  const rawOffset = params.get("offset") || "0";
  const offset =
    /^\d+$/.test(rawOffset) && Number(rawOffset) <= 10000
      ? Number(rawOffset)
      : 0;
  const [creating, setCreating] = useState(false);
  const [reviewing, setReviewing] = useState<FeedbackEntry>();
  const [placeSearch, setPlaceSearch] = useState("");
  const [form] = Form.useForm<CreateFeedbackRequest>();
  const [reviewForm] = Form.useForm<ReviewFeedbackRequest>();
  const nextStatus = Form.useWatch("status", reviewForm);
  const terminal = nextStatus === "resolved" || nextStatus === "dismissed";
  const query = useListPlatformFeedback(
    { status, category, offset, limit: 20 },
    { fetch: auth.fetchOptions, query: { retry: false } },
  );
  const catalogue = useListPlatformFeedbackCategories({
    fetch: auth.fetchOptions,
  });
  const categories = catalogue.data?.status === 200 ? catalogue.data.data : [];
  const places = useListPlatformPlaces(
    { q: placeSearch, limit: 50 },
    { fetch: auth.fetchOptions, query: { enabled: creating } },
  );
  const placeOptions =
    places.data?.status === 200 ? places.data.data.items : [];
  const options = {
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
  };
  const saved = async (response: { status: number }) => {
    if (response.status >= 300) {
      void message.error(
        response.status === 409
          ? "记录已变化或状态转换不允许，请刷新后重试"
          : "操作失败，请检查输入并重试",
      );
      return;
    }
    setCreating(false);
    setReviewing(undefined);
    await client.invalidateQueries({
      queryKey: getListPlatformFeedbackQueryKey(),
    });
  };
  const create = useCreatePlatformFeedback({
    ...options,
    mutation: {
      onSuccess: saved,
      onError: () => void message.error("录入失败，请重试"),
    },
  });
  const review = useReviewPlatformFeedback({
    ...options,
    mutation: {
      onSuccess: saved,
      onError: () => void message.error("处理失败，请重试"),
    },
  });
  const page = query.data?.status === 200 ? query.data.data : undefined;
  const move = (newOffset: number) => {
    const next = new URLSearchParams(params);
    next.set("offset", String(newOffset));
    setParams(next);
  };
  const transitions: FeedbackStatus[] =
    reviewing?.status === "open"
      ? ["in_review", "dismissed"]
      : reviewing?.status === "in_review"
        ? ["resolved", "dismissed"]
        : ["in_review"];
  return (
    <Card
      title="反馈处理"
      extra={
        <Button
          type="primary"
          onClick={() => {
            form.resetFields();
            setCreating(true);
          }}
        >
          人工录入
        </Button>
      }
    >
      <Typography.Paragraph type="secondary">
        仅处理人工录入的产品反馈。账户、个人数据和纠纷须使用后续专用流程；请勿录入密码、证明材料或个人联系方式。反馈原文不公开。
      </Typography.Paragraph>
      <Form
        key={`${status}/${category}`}
        layout="inline"
        initialValues={{ status, category }}
        onFinish={(values: { status?: string; category?: string }) => {
          const next = new URLSearchParams();
          if (values.status) next.set("status", values.status);
          if (values.category) next.set("category", values.category);
          setParams(next);
        }}
      >
        <Form.Item name="status" label="状态">
          <Select
            allowClear
            style={{ width: 150 }}
            options={Object.entries(labels).map(([value, label]) => ({
              value,
              label,
            }))}
          />
        </Form.Item>
        <Form.Item name="category" label="分类">
          <Select
            allowClear
            style={{ width: 160 }}
            options={categories.map((c) => ({ value: c.key, label: c.label }))}
          />
        </Form.Item>
        <Button htmlType="submit">筛选</Button>
        <Button onClick={() => setParams({})}>清除筛选</Button>
      </Form>
      {(query.isError || (query.data && query.data.status !== 200)) && (
        <Alert
          type="error"
          title="反馈加载失败"
          action={<Button onClick={() => void query.refetch()}>重试</Button>}
        />
      )}
      <List
        loading={query.isLoading}
        dataSource={page?.items ?? []}
        locale={{ emptyText: "暂无反馈" }}
        renderItem={(entry) => (
          <List.Item
            actions={[
              <Button
                key="review"
                onClick={() => {
                  reviewForm.resetFields();
                  setReviewing(entry);
                }}
              >
                处理
              </Button>,
            ]}
          >
            <List.Item.Meta
              title={
                <Space>
                  {entry.title}
                  <Tag>{labels[entry.status]}</Tag>
                  <Tag>
                    {categories.find((c) => c.key === entry.category)?.label ||
                      entry.category}
                  </Tag>
                </Space>
              }
              description={
                <>
                  <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>
                    {entry.message}
                  </Typography.Paragraph>
                  {entry.outcome && (
                    <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>
                      处理结果：{entry.outcome}
                    </Typography.Paragraph>
                  )}
                  {entry.placeId && (
                    <Typography.Text type="secondary">
                      关联地点：{entry.placeId}
                    </Typography.Text>
                  )}
                </>
              }
            />
          </List.Item>
        )}
      />
      <Space>
        <Button
          disabled={offset === 0}
          onClick={() => move(Math.max(0, offset - 20))}
        >
          上一页
        </Button>
        <Button
          disabled={page?.nextOffset == null}
          onClick={() => move(page?.nextOffset ?? offset)}
        >
          下一页
        </Button>
      </Space>
      <Modal
        destroyOnHidden
        title="人工录入反馈"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(values) =>
            create.mutate({
              data: { ...values, placeId: values.placeId || null },
            })
          }
        >
          <Form.Item
            name="category"
            label="原始分类"
            rules={[{ required: true }]}
          >
            <Select
              options={categories.map((c) => ({
                value: c.key,
                label: c.label,
              }))}
            />
          </Form.Item>
          <Form.Item name="placeId" label="关联地点（可留空）">
            <Select
              showSearch
              allowClear
              filterOption={false}
              onSearch={setPlaceSearch}
              options={placeOptions.map((p) => ({
                value: p.id,
                label: `${p.details.name} (${p.slug})`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="title"
            label="标题"
            rules={[{ required: true, whitespace: true, max: 200 }]}
          >
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item
            name="message"
            label="反馈内容"
            rules={[{ required: true, whitespace: true, max: 5000 }]}
          >
            <Input.TextArea rows={5} maxLength={5000} />
          </Form.Item>
          <Form.Item
            name="reason"
            label="录入理由（勿填写反馈原文或个人信息）"
            rules={[{ required: true, whitespace: true, max: 500 }]}
          >
            <Input maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        destroyOnHidden
        title="处理反馈"
        open={Boolean(reviewing)}
        onCancel={() => setReviewing(undefined)}
        onOk={() => reviewForm.submit()}
        confirmLoading={review.isPending}
      >
        <Form
          form={reviewForm}
          layout="vertical"
          onFinish={(values) => {
            if (reviewing)
              review.mutate({
                id: reviewing.id,
                data: {
                  ...values,
                  expectedVersion: reviewing.version,
                  outcome: terminal ? values.outcome : null,
                },
              });
          }}
        >
          <Form.Item
            name="status"
            label="处理状态"
            rules={[{ required: true }]}
          >
            <Select
              options={transitions.map((value) => ({
                value,
                label: labels[value],
              }))}
            />
          </Form.Item>
          {terminal && (
            <Form.Item
              name="outcome"
              label="实际处理结果或不处理依据"
              rules={[{ required: true, whitespace: true, max: 2000 }]}
            >
              <Input.TextArea rows={4} maxLength={2000} />
            </Form.Item>
          )}
          <Form.Item
            name="reason"
            label="审核理由（勿填写个人信息）"
            rules={[{ required: true, whitespace: true, max: 500 }]}
          >
            <Input maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
