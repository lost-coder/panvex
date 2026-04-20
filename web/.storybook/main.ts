import type { StorybookConfig } from "@storybook/react-vite";

const config: StorybookConfig = {
  stories: ["../src/**/*.stories.@(ts|tsx)"],
  addons: ["@storybook/addon-a11y"],
  framework: "@storybook/react-vite",
  async viteFinal(config) {
    config.plugins = config.plugins?.filter(
      (plugin) => plugin && "name" in plugin && plugin.name !== "vite:dts",
    );
    return config;
  },
};

export default config;
