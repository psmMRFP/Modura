import { createBrowserRouter, Navigate } from "react-router-dom";
import { PlacePage, PlacesPage } from "../features/places/PlacePages";
import { Home, LocaleLayout, NotFound } from "./pages";

export const router = createBrowserRouter([
  { path: "/", element: <Navigate to="/en" replace /> },
  {
    path: "/:locale",
    element: <LocaleLayout />,
    children: [
      { index: true, element: <Home /> },
      { path: "places", element: <PlacesPage /> },
      { path: "places/:slug", element: <PlacePage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);
