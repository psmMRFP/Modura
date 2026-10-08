import { createBrowserRouter } from "react-router-dom";

import { LoginPage } from "../features/auth/LoginPage";
import { PlatformLayout } from "../features/platform/PlatformLayout";
import { PlatformLoginPage } from "../features/platform/PlatformLoginPage";
import { AdminLayout } from "../features/workspace/AdminLayout";
import { PermissionGuard } from "../features/workspace/PermissionGuard";
import { routePermissions } from "../features/workspace/navigation";
import {
  AuditRoute,
  ConfigurationsRoute,
  DictionariesRoute,
  DepartmentsRoute,
  PositionsRoute,
  PlatformAuditRoute,
  PlatformPlacesRoute,
  PlatformConfigurationsRoute,
  PlatformDictionariesRoute,
  PlatformTenantsRoute,
  ProfileRoute,
  RolePoliciesRoute,
  RolesRoute,
  UserAssignmentsRoute,
  WorkspaceRoute,
} from "./route-pages";

export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  { path: "/platform/login", element: <PlatformLoginPage /> },
  {
    path: "/platform",
    element: <PlatformLayout />,
    children: [
      { index: true, element: <PlatformTenantsRoute /> },
      { path: "audit", element: <PlatformAuditRoute /> },
      { path: "places", element: <PlatformPlacesRoute /> },
      { path: "settings/dictionaries", element: <PlatformDictionariesRoute /> },
      {
        path: "settings/configurations",
        element: <PlatformConfigurationsRoute />,
      },
    ],
  },
  {
    path: "/",
    element: <AdminLayout />,
    children: [
      { index: true, element: <WorkspaceRoute /> },
      { path: "profile", element: <ProfileRoute /> },
      {
        path: "organization/departments",
        element: (
          <PermissionGuard permission={routePermissions.departments}>
            <DepartmentsRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "organization/positions",
        element: (
          <PermissionGuard permission={routePermissions.positions}>
            <PositionsRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "organization/users",
        element: (
          <PermissionGuard permission={routePermissions.users}>
            <UserAssignmentsRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "authorization/roles",
        element: (
          <PermissionGuard permission={routePermissions.roles}>
            <RolesRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "authorization/roles/:roleId/policies",
        element: (
          <PermissionGuard permission={routePermissions.rolePolicies}>
            <RolePoliciesRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "settings/dictionaries",
        element: (
          <PermissionGuard permission={routePermissions.dictionaries}>
            <DictionariesRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "settings/configurations",
        element: (
          <PermissionGuard permission={routePermissions.configurations}>
            <ConfigurationsRoute />
          </PermissionGuard>
        ),
      },
      {
        path: "audit",
        element: (
          <PermissionGuard permission={routePermissions.audit}>
            <AuditRoute />
          </PermissionGuard>
        ),
      },
    ],
  },
]);
