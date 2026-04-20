import type { Meta, StoryObj } from "@storybook/react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./tabs";

const meta: Meta<typeof Tabs> = {
  title: "UI/Tabs",
  component: Tabs,
};
export default meta;

type Story = StoryObj<typeof Tabs>;

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="activity" className="w-[340px]">
      <TabsList>
        <TabsTrigger value="activity">Activity</TabsTrigger>
        <TabsTrigger value="actions">Actions</TabsTrigger>
        <TabsTrigger value="config">Config</TabsTrigger>
      </TabsList>
      <TabsContent value="activity">
        <p className="text-fg-muted text-sm">Recent events and timeline…</p>
      </TabsContent>
      <TabsContent value="actions">
        <p className="text-fg-muted text-sm">Quick actions panel…</p>
      </TabsContent>
      <TabsContent value="config">
        <p className="text-fg-muted text-sm">Configuration options…</p>
      </TabsContent>
    </Tabs>
  ),
};
