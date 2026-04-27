import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./styles/atlas.css";
import "./styles/app.css";
import "./styles/pool.css";
import "./styles/routing.css";
import "./styles/folio.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
