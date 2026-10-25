import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App.tsx";
import { AuthProvider } from "./AuthProvider.tsx";
import SiteRoot from "./components/SiteRoot.tsx";
import { BASE_PATH } from "./basePath.ts";

// An empty base path is the site outside every Organisation (the server leaves
// <base> at the root there), so none of the Organisation's app — its session
// check included — runs.
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {BASE_PATH === "" ? (
      <SiteRoot pathname={window.location.pathname} />
    ) : (
      <AuthProvider>
        <App />
      </AuthProvider>
    )}
  </StrictMode>,
);
