import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Form,
  Input,
  message,
  Modal,
  Space,
  Table,
  Tag,
} from "antd";
import { useState } from "react";
import {
  getListPlatformTenantsQueryKey,
  useListPlatformTenants,
  useProvisionPlatformTenant,
  useReactivatePlatformTenant,
  useSuspendPlatformTenant,
  useUpdatePlatformTenant,
  type PlatformTenant,
} from "../../api/generated/modura";
import { usePlatformAuth } from "./platform-auth-context";

export function TenantsPage() {
  const auth = usePlatformAuth();
  const client = useQueryClient();
  const query = useListPlatformTenants({ fetch: auth.fetchOptions });
  const [provisioningKey, setProvisioningKey] = useState(() =>
    crypto.randomUUID(),
  );
  const [editing, setEditing] = useState<PlatformTenant>();
  const [editForm] = Form.useForm();
  const tenants = query.data?.status === 200 ? query.data.data : [];
  const writeFetch = {
    ...auth.fetchOptions,
    headers: {
      ...auth.fetchOptions.headers,
      "X-CSRF-Token": auth.csrfToken,
    },
  };
  const provisioningFetch = {
    ...writeFetch,
    headers: { ...writeFetch.headers, "Idempotency-Key": provisioningKey },
  };
  const refresh = () =>
    client.invalidateQueries({ queryKey: getListPlatformTenantsQueryKey() });
  const provision = useProvisionPlatformTenant({
    fetch: provisioningFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200 || response.status === 201) {
          setProvisioningKey(crypto.randomUUID());
          message.success(
            response.data.created ? "租户已创建" : "幂等请求已返回原结果",
          );
          await refresh();
        } else message.error("创建租户失败");
      },
    },
  });
  const update = useUpdatePlatformTenant({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("租户资料已更新");
          setEditing(undefined);
          await refresh();
        } else if (response.status === 409) {
          message.warning("租户资料已被其他管理员修改，请刷新后重试");
        } else message.error("更新租户失败");
      },
    },
  });
  const suspend = useSuspendPlatformTenant({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("租户已暂停");
          await refresh();
        } else message.error("操作失败");
      },
    },
  });
  const reactivate = useReactivatePlatformTenant({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("租户已恢复");
          await refresh();
        } else message.error("操作失败");
      },
    },
  });
  const lifecycle = (tenantId: string, action: "suspend" | "reactivate") => {
    let reason = "";
    Modal.confirm({
      title: action === "suspend" ? "暂停租户" : "恢复租户",
      content: (
        <Input.TextArea
          placeholder="请输入审计原因"
          onChange={(event) => {
            reason = event.target.value;
          }}
        />
      ),
      onOk: () => {
        if (!reason.trim()) {
          message.warning("必须填写原因");
          return Promise.reject();
        }
        return action === "suspend"
          ? suspend.mutateAsync({ tenantId, data: { reason } })
          : reactivate.mutateAsync({ tenantId, data: { reason } });
      },
    });
  };
  return (
    <Space direction="vertical" size="large" className="workspace">
      <Card title="创建租户">
        <Form layout="vertical" onFinish={(data) => provision.mutate({ data })}>
          <Space wrap align="start">
            <Form.Item
              name="slug"
              label="租户标识"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="displayName"
              label="显示名称"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="rootDepartmentName"
              label="根部门"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="administratorUsername"
              label="首位管理员"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="administratorEmail" label="管理员邮箱">
              <Input />
            </Form.Item>
            <Form.Item
              name="reason"
              label="创建原因"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
          </Space>
          <Button
            type="primary"
            htmlType="submit"
            loading={provision.isPending}
          >
            原子创建租户
          </Button>
        </Form>
      </Card>
      <Card title="租户">
        <Table
          rowKey="id"
          loading={query.isLoading}
          dataSource={tenants}
          pagination={false}
          columns={[
            { title: "名称", dataIndex: "displayName" },
            { title: "标识", dataIndex: "slug" },
            {
              title: "状态",
              render: (_, tenant) => (
                <Tag
                  color={
                    tenant.status === "active"
                      ? "green"
                      : tenant.status === "suspended"
                        ? "orange"
                        : "blue"
                  }
                >
                  {tenant.status}
                </Tag>
              ),
            },
            {
              title: "操作",
              render: (_, tenant) => (
                <Space>
                  <Button
                    onClick={() => {
                      setEditing(tenant);
                      editForm.setFieldsValue({
                        displayName: tenant.displayName,
                        reason: "",
                      });
                    }}
                  >
                    编辑资料
                  </Button>
                  {tenant.status === "active" ? (
                    <Button
                      danger
                      onClick={() => lifecycle(tenant.id, "suspend")}
                    >
                      暂停
                    </Button>
                  ) : tenant.status === "suspended" ? (
                    <Button onClick={() => lifecycle(tenant.id, "reactivate")}>
                      恢复
                    </Button>
                  ) : null}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title="编辑租户资料"
        open={Boolean(editing)}
        confirmLoading={update.isPending}
        onCancel={() => setEditing(undefined)}
        onOk={() => editForm.submit()}
        destroyOnHidden
      >
        <Form
          form={editForm}
          layout="vertical"
          onFinish={(data: { displayName: string; reason: string }) =>
            editing &&
            update.mutate({
              tenantId: editing.id,
              data: {
                ...data,
                expectedUpdatedAt: editing.updatedAt,
              },
            })
          }
        >
          <Form.Item
            name="displayName"
            label="显示名称"
            rules={[{ required: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="reason"
            label="修改原因"
            rules={[{ required: true }]}
          >
            <Input.TextArea maxLength={512} showCount />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
