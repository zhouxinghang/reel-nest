import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, Link, RouterProvider } from "react-router-dom";
import { createAPI } from "../shared/api";
import { APIProvider, createQueryClient } from "../shared/services";
import { App } from "./App";
import { Login } from "../features/identity/Login";
import { Home } from "../features/catalog/Home";
import { Browse } from "../features/catalog/Browse";
import {
  MediaDetail,
  PersonDetail,
  SeriesDetail,
} from "../features/catalog/Details";
import { Settings } from "../features/settings/Settings";
import { Libraries } from "../features/libraries/Libraries";
import { Tasks } from "../features/tasks/Tasks";
import { Actors } from "../features/actors/Actors";
import "./tokens.css";

const client = createQueryClient();
const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  {
    element: <App />,
    children: [
      { index: true, element: <Home /> },
      { path: "/library", element: <Browse /> },
      { path: "/media/:id", element: <MediaDetail /> },
      { path: "/series/:id", element: <SeriesDetail /> },
      { path: "/people/:id", element: <PersonDetail /> },
      { path: "/actors", element: <Actors /> },
      { path: "/libraries", element: <Libraries /> },
      { path: "/tasks", element: <Tasks /> },
      { path: "/settings/metadata", element: <Settings /> },
      {
        path: "*",
        element: (
          <>
            <h1>页面不存在</h1>
            <Link to="/">返回首页</Link>
          </>
        ),
      },
    ],
  },
]);
const api = createAPI(() => {
  const location = router.state.location;
  if (location.pathname === "/login") return;
  const next = location.pathname + location.search;
  // Cancel reads before returning to login, so an old response cannot repopulate
  // the household cache after a subsequent successful login.
  api.cancelAll();
  void client.cancelQueries();
  client.clear();
  void router.navigate(`/login?${new URLSearchParams({ next })}`, {
    replace: true,
  });
});
createRoot(document.getElementById("root")!).render(
  <APIProvider value={api}>
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </APIProvider>,
);
