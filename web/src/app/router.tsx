import { createBrowserRouter, Navigate } from "react-router-dom";
import { PlacePage, PlacesPage } from "../features/places/PlacePages";
import { lazy, Suspense } from "react";
const AuthPage = lazy(() =>
  import("../features/auth/AuthPage").then((module) => ({
    default: module.AuthPage,
  })),
);
import { authRoutes } from "../features/auth/routes";
import { Home, LocaleLayout, NotFound } from "./pages";

export const router = createBrowserRouter([
  { path: "/", element: <Navigate to="/en" replace /> },
  {
    path: "/:locale",
    element: <LocaleLayout />,
    children: [
      { index: true, element: <Home /> },
      ...authRoutes.map((path) => ({
        path,
        element: (
          <Suspense fallback={<p role="status">…</p>}>
            <AuthPage key={path} />
          </Suspense>
        ),
      })),
      { path: "places", element: <PlacesPage /> },
      { path: "places/:slug", element: <PlacePage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);
