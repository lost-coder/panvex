import type { Preview } from "@storybook/react";
import "../src/index.css";

const preview: Preview = {
  parameters: {
    backgrounds: {
      default: "ops-dark",
      values: [
        { name: "ops-dark", value: "#0b0d12" },
        { name: "ops-light", value: "#f5f6f8" },
        { name: "card", value: "#141820" },
      ],
    },
    viewport: {
      viewports: {
        mobile: { name: "Mobile", styles: { width: "375px", height: "812px" } },
        tablet: { name: "Tablet", styles: { width: "768px", height: "1024px" } },
        desktop: { name: "Desktop", styles: { width: "1440px", height: "900px" } },
      },
    },
    layout: "centered",
  },
};

export default preview;
