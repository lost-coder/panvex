import React from "react";
import ReactDOM from "react-dom/client";
import "./styles.css";

function TokenTest() {
  return (
    <div style={{ padding: "2rem" }}>
      <h1 style={{ color: "var(--text-1)", fontFamily: "var(--font)", fontSize: "22px", fontWeight: 800 }}>
        Token Test
      </h1>
      <div style={{ display: "flex", gap: "8px", marginTop: "1rem" }}>
        <div style={{ width: 40, height: 40, background: "var(--accent)", borderRadius: "var(--radius-xs)" }} />
        <div style={{ width: 40, height: 40, background: "var(--good)", borderRadius: "var(--radius-xs)" }} />
        <div style={{ width: 40, height: 40, background: "var(--warn)", borderRadius: "var(--radius-xs)" }} />
        <div style={{ width: 40, height: 40, background: "var(--bad)", borderRadius: "var(--radius-xs)" }} />
      </div>
      <p style={{ color: "var(--text-2)", marginTop: "1rem" }}>
        If you see colored squares and this text, tokens work.
      </p>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <TokenTest />
  </React.StrictMode>
);
