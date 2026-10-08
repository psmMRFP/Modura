import { Spin } from "antd";
import { lazy, Suspense, type ComponentType } from "react";

function deferred<T extends object>(
  load: () => Promise<T>,
  select: (module: T) => ComponentType,
) {
  const Page = lazy(async () => ({ default: select(await load()) }));
  return function DeferredPage() {
    return (
      <Suspense fallback={<Spin fullscreen />}>
        <Page />
      </Suspense>
    );
  };
}

export const AuditRoute = deferred(
  () => import("../features/audit/AuditPage"),
  (module) => module.AuditPage,
);
export const ConfigurationsRoute = deferred(
  () => import("../features/settings/ConfigurationsPage"),
  (module) => module.ConfigurationsPage,
);
export const DictionariesRoute = deferred(
  () => import("../features/settings/DictionariesPage"),
  (module) => module.DictionariesPage,
);
export const WorkspaceRoute = deferred(
  () => import("../features/workspace/Workspace"),
  (module) => module.Workspace,
);
export const ProfileRoute = deferred(
  () => import("../features/auth/ProfilePage"),
  (module) => module.ProfilePage,
);
export const DepartmentsRoute = deferred(
  () => import("../features/organization/DepartmentsPage"),
  (module) => module.DepartmentsPage,
);
export const PositionsRoute = deferred(
  () => import("../features/organization/PositionsPage"),
  (module) => module.PositionsPage,
);
export const UserAssignmentsRoute = deferred(
  () => import("../features/organization/UserAssignmentsPage"),
  (module) => module.UserAssignmentsPage,
);
export const RolesRoute = deferred(
  () => import("../features/authorization/RolesPage"),
  (module) => module.RolesPage,
);
export const RolePoliciesRoute = deferred(
  () => import("../features/authorization/RolePoliciesPage"),
  (module) => module.RolePoliciesPage,
);
export const PlatformTenantsRoute = deferred(
  () => import("../features/platform/TenantsPage"),
  (module) => module.TenantsPage,
);
export const PlatformAuditRoute = deferred(
  () => import("../features/platform/PlatformAuditPage"),
  (module) => module.PlatformAuditPage,
);
export const PlatformDictionariesRoute = deferred(
  () => import("../features/platform/PlatformDictionariesPage"),
  (module) => module.PlatformDictionariesPage,
);
export const PlatformConfigurationsRoute = deferred(
  () => import("../features/platform/PlatformConfigurationsPage"),
  (module) => module.PlatformConfigurationsPage,
);

export const PlatformPlacesRoute = deferred(
  () => import("../features/places/PlatformPlacesPage"),
  (module) => module.PlatformPlacesPage,
);

export const PlatformFeedbackRoute = deferred(
  () => import("../features/feedback/PlatformFeedbackPage"),
  (module) => module.PlatformFeedbackPage,
);
