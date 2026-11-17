import React from "react";
import { createRoot } from "react-dom/client";
import { Latest } from "./features/holdings/Latest";
import "./style.css";
const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <React.StrictMode>
      <main>
        <header>
          <small>BIFROST · 合成数据演示</small>
          <h1>最新采集</h1>
        </header>
        <Latest />
      </main>
    </React.StrictMode>,
  );
