import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  Form,
  message,
  Select,
  Space,
  Spin,
  Typography,
} from "antd";
import { useParams } from "react-router-dom";
import {
  DataScopeKind,
  getGetRolePolicySetQueryKey,
  RolePolicyAction,
  RolePolicyResource,
  useGetRolePolicySet,
  useListDepartments,
  useReplaceRolePolicies,
  type RolePolicy,
} from "../../api/generated/wheretolive";
import { useAuth } from "../auth/auth-context";
import { usePermissions } from "../workspace/use-permissions";

const resources = Object.values(RolePolicyResource).map((value) => ({
  value,
  label: value,
}));
const actions = Object.values(RolePolicyAction).map((value) => ({
  value,
  label: value,
}));
const scopes = Object.values(DataScopeKind).map((value) => ({
  value,
  label: value,
}));

export function RolePoliciesPage() {
  const roleId = useParams().roleId ?? "";
  const [form] = Form.useForm();
  const auth = useAuth();
  const granted = usePermissions();
  const client = useQueryClient();
  const query = useGetRolePolicySet(roleId, {
    fetch: auth.fetchOptions,
    query: { enabled: Boolean(roleId) },
  });
  const state = query.data?.status === 200 ? query.data.data : undefined;
  const departmentsQuery = useListDepartments({ fetch: auth.fetchOptions });
  const departments =
    departmentsQuery.data?.status === 200
      ? departmentsQuery.data.data.map((department) => ({
          value: department.id,
          label: department.name,
        }))
      : [];
  const replace = useReplaceRolePolicies({
    fetch: {
      ...auth.fetchOptions,
      headers: { ...auth.fetchOptions.headers, "X-CSRF-Token": auth.csrfToken },
    },
    mutation: {
      onSuccess: async (response) => {
        if (response.status === 200) {
          message.success("策略已更新");
          await client.invalidateQueries({
            queryKey: getGetRolePolicySetQueryKey(roleId),
          });
        } else if (response.status === 409)
          message.warning("策略已被其他管理员修改，请刷新后重试");
        else message.error("更新失败");
      },
    },
  });
  if (query.isLoading) return <Spin />;
  if (!state)
    return (
      <Card>
        <Typography.Text type="danger">无法读取角色策略</Typography.Text>
      </Card>
    );
  const editable =
    !state.reserved && granted.has("authorization.policies/update");
  return (
    <Card
      title="角色策略"
      extra={
        <Typography.Text type="secondary">版本 {state.version}</Typography.Text>
      }
    >
      <Form
        form={form}
        key={state.version}
        initialValues={{ policies: state.policies }}
        onFinish={(value: { policies?: RolePolicy[] }) =>
          replace.mutate({
            roleId,
            data: {
              expectedVersion: state.version,
              policies: value.policies ?? [],
            },
          })
        }
      >
        <Form.List name="policies">
          {(fields, { add, remove }) => (
            <Space direction="vertical" className="workspace">
              {fields.map((field) => (
                <Form.Item
                  key={field.key}
                  noStyle
                  shouldUpdate={(previous, current) => {
                    const before = previous.policies?.[field.name];
                    const after = current.policies?.[field.name];
                    return (
                      before?.resource !== after?.resource ||
                      before?.dataScope !== after?.dataScope
                    );
                  }}
                >
                  {({ getFieldValue }) => {
                    const policy = getFieldValue(["policies", field.name]);
                    const supportsNarrowScope =
                      policy?.resource === "organization.departments" ||
                      policy?.resource === "organization.user-organization";
                    const scopeOptions = supportsNarrowScope
                      ? scopes
                      : scopes.filter((scope) => scope.value === "all");
                    return (
                      <Space wrap>
                        <Form.Item
                          {...field}
                          name={[field.name, "resource"]}
                          rules={[{ required: true }]}
                        >
                          <Select
                            disabled={!editable}
                            placeholder="资源"
                            style={{ width: 260 }}
                            options={resources}
                            onChange={(resource) => {
                              if (
                                resource !== "organization.departments" &&
                                resource !== "organization.user-organization"
                              ) {
                                form.setFieldValue(
                                  ["policies", field.name, "dataScope"],
                                  "all",
                                );
                                form.setFieldValue(
                                  ["policies", field.name, "departmentIds"],
                                  undefined,
                                );
                              }
                            }}
                          />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, "action"]}
                          rules={[{ required: true }]}
                        >
                          <Select
                            disabled={!editable}
                            placeholder="动作"
                            style={{ width: 120 }}
                            options={actions}
                          />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, "dataScope"]}
                          rules={[{ required: true }]}
                        >
                          <Select
                            disabled={!editable}
                            placeholder="数据范围"
                            style={{ width: 220 }}
                            options={scopeOptions}
                            onChange={(scope) => {
                              if (scope !== "custom")
                                form.setFieldValue(
                                  ["policies", field.name, "departmentIds"],
                                  undefined,
                                );
                            }}
                          />
                        </Form.Item>
                        {policy?.dataScope === "custom" && (
                          <Form.Item
                            {...field}
                            name={[field.name, "departmentIds"]}
                            rules={[
                              {
                                required: true,
                                type: "array",
                                min: 1,
                                message: "请选择至少一个部门",
                              },
                            ]}
                          >
                            <Select
                              mode="multiple"
                              disabled={!editable}
                              loading={departmentsQuery.isLoading}
                              placeholder="自定义部门"
                              style={{ minWidth: 260 }}
                              options={departments}
                            />
                          </Form.Item>
                        )}
                        {editable && (
                          <Button danger onClick={() => remove(field.name)}>
                            移除
                          </Button>
                        )}
                      </Space>
                    );
                  }}
                </Form.Item>
              ))}
              {editable && (
                <Space>
                  <Button onClick={() => add({ dataScope: "all" })}>
                    添加策略
                  </Button>
                  <Button
                    type="primary"
                    htmlType="submit"
                    loading={replace.isPending}
                  >
                    保存期望状态
                  </Button>
                </Space>
              )}
            </Space>
          )}
        </Form.List>
        {state.reserved && (
          <Typography.Text type="secondary">
            系统保留角色不可由租户修改。
          </Typography.Text>
        )}
      </Form>
    </Card>
  );
}
