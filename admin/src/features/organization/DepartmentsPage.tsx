import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
} from "antd";
import { useState } from "react";

import {
  getListDepartmentsQueryKey,
  useCreateDepartment,
  useDeleteDepartment,
  useListDepartments,
  useMoveDepartment,
  useUpdateDepartment,
  type Department,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

export function DepartmentsPage() {
  const [editing, setEditing] = useState<Department>();
  const [editForm] = Form.useForm();
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const query = useListDepartments({ fetch: auth.fetchOptions });
  const departments = query.data?.status === 200 ? query.data.data : [];
  const writeFetch = {
    ...auth.fetchOptions,
    headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
  };
  const refresh = () =>
    client.invalidateQueries({ queryKey: getListDepartmentsQueryKey() });
  const create = useCreateDepartment({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 201) {
          message.success("部门已创建");
          await refresh();
        } else message.error("创建失败");
      },
    },
  });
  const move = useMoveDepartment({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("部门已移动");
          await refresh();
        } else message.error("移动失败");
      },
    },
  });
  const update = useUpdateDepartment({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("部门已更新");
          setEditing(undefined);
          await refresh();
        } else message.error("更新失败");
      },
    },
  });
  const remove = useDeleteDepartment({
    fetch: writeFetch,
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 204) {
          message.success("部门已删除");
          await refresh();
        } else message.error("删除失败");
      },
    },
  });

  return (
    <Space direction="vertical" size="large" className="workspace">
      {granted.has("organization.departments/create") && (
        <Card title="创建部门">
          <Form
            layout="inline"
            onFinish={(value) => create.mutate({ data: value })}
          >
            <Form.Item
              name="parentId"
              label="上级部门"
              rules={[{ required: true }]}
            >
              <Select
                style={{ width: 220 }}
                options={departments.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
              />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input maxLength={128} />
            </Form.Item>
            <Form.Item name="sortOrder" label="排序" initialValue={0}>
              <InputNumber />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={create.isPending}>
              创建
            </Button>
          </Form>
        </Card>
      )}
      <Card title="部门">
        <Table
          rowKey="id"
          loading={query.isLoading}
          dataSource={departments}
          pagination={false}
          columns={[
            { title: "名称", dataIndex: "name" },
            { title: "排序", dataIndex: "sortOrder" },
            {
              title: "类型",
              render: (_, item) =>
                item.parentId ? (
                  <Tag>部门</Tag>
                ) : (
                  <Tag color="blue">根部门</Tag>
                ),
            },
            {
              title: "操作",
              render: (_, item) => (
                <Space>
                  {item.parentId &&
                    granted.has("organization.departments/update") && (
                      <Select
                        placeholder="移动到"
                        style={{ width: 160 }}
                        options={departments
                          .filter((parent) => parent.id !== item.id)
                          .map((parent) => ({
                            value: parent.id,
                            label: parent.name,
                          }))}
                        onChange={(parentId) =>
                          move.mutate({
                            departmentId: item.id,
                            data: { parentId },
                          })
                        }
                      />
                    )}
                  {granted.has("organization.departments/update") && (
                    <Button
                      onClick={() => {
                        setEditing(item);
                        editForm.setFieldsValue({
                          name: item.name,
                          sortOrder: item.sortOrder,
                        });
                      }}
                    >
                      编辑
                    </Button>
                  )}
                  {item.parentId &&
                    granted.has("organization.departments/delete") && (
                      <Button
                        danger
                        loading={remove.isPending}
                        onClick={() => remove.mutate({ departmentId: item.id })}
                      >
                        删除
                      </Button>
                    )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title="编辑部门"
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
            editing && update.mutate({ departmentId: editing.id, data })
          }
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item name="sortOrder" label="排序" rules={[{ required: true }]}>
            <InputNumber />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
