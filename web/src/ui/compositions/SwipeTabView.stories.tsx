import type { Meta, StoryObj } from "@storybook/react";
import { SwipeTabView } from "./SwipeTabView";
import { Timeline } from "./Timeline";

const meta: Meta<typeof SwipeTabView> = {
  title: "Compositions/SwipeTabView",
  component: SwipeTabView,
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj<typeof SwipeTabView>;

export const Default: Story = {
  render: () => (
    <div className="max-w-md mx-auto h-[600px] bg-bg">
      <SwipeTabView
        tabs={[
          {
            id: "overview",
            label: "Overview",
            content: (
              <div className="p-4 space-y-3">
                <div className="rounded-xs bg-bg-card border border-border p-4 text-center">
                  <span className="text-2xl font-mono font-bold text-fg">7/9</span>
                  <span className="text-xs text-fg-muted block mt-1">servers online</span>
                </div>
                <div className="rounded-xs bg-status-error/10 border border-status-error/30 p-3">
                  <span className="text-xs text-status-error">
                    ⚠ node-fr-03 — DC4, DC5 unreachable
                  </span>
                </div>
              </div>
            ),
          },
          {
            id: "timeline",
            label: "Timeline",
            content: (
              <div className="p-4">
                <Timeline
                  events={[
                    { status: "error", time: "14:23", message: "DC4 unreachable on node-fr-03" },
                    { status: "warn", time: "14:20", message: "DC2 degraded on node-de-01" },
                    { status: "ok", time: "13:51", message: "node-de-01 recovered" },
                  ]}
                />
              </div>
            ),
          },
        ]}
      />
    </div>
  ),
};

export const SwipeDisabled: Story = {
  name: "Swipe Disabled (tap only)",
  render: () => (
    <div className="max-w-md mx-auto h-[400px] bg-bg">
      <SwipeTabView
        swipeEnabled={false}
        tabs={[
          {
            id: "tab1",
            label: "Tab 1",
            content: <div className="p-4 text-fg">Tab 1 — no swipe, tap to switch</div>,
          },
          { id: "tab2", label: "Tab 2", content: <div className="p-4 text-fg">Tab 2 content</div> },
          { id: "tab3", label: "Tab 3", content: <div className="p-4 text-fg">Tab 3 content</div> },
        ]}
      />
    </div>
  ),
};
