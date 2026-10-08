export const routePermissions = {
  users: "identity.users/read",
  departments: "organization.departments/read",
  positions: "organization.positions/read",
  roles: "authorization.roles/read",
  rolePolicies: "authorization.policies/read",
  dictionaries: "settings.dictionaries/read",
  configurations: "settings.configurations/read",
  audit: "audit.events/read",
} as const;

export const navigation = [
  { key: "/", label: "概览" },
  { key: "/profile", label: "个人资料" },
  {
    key: "/organization/departments",
    label: "部门管理",
    permission: routePermissions.departments,
  },
  {
    key: "/organization/positions",
    label: "岗位管理",
    permission: routePermissions.positions,
  },
  {
    key: "/organization/users",
    label: "用户管理",
    permission: routePermissions.users,
  },
  {
    key: "/authorization/roles",
    label: "角色与策略",
    permission: routePermissions.roles,
  },
  {
    key: "/settings/dictionaries",
    label: "字典管理",
    permission: routePermissions.dictionaries,
  },
  {
    key: "/settings/configurations",
    label: "系统配置",
    permission: routePermissions.configurations,
  },
  { key: "/audit", label: "审计日志", permission: routePermissions.audit },
];

export function visibleNavigation(granted: ReadonlySet<string>) {
  return navigation.filter(
    (item) => !item.permission || granted.has(item.permission),
  );
}
