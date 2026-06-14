import type { Meta, StoryObj } from "@storybook/react";
import { Popover, PopoverTrigger, PopoverContent } from "./popover";
import { Button } from "./button";
import { IPTag } from "@/ui/primitives/IPTag";

const meta: Meta = { title: "UI/Popover" };
export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          Show Links
        </Button>
      </PopoverTrigger>
      <PopoverContent>
        <div className="flex flex-col gap-2">
          <h4 className="text-sm font-medium text-fg">Connection Links</h4>
          <div className="flex flex-col gap-1">
            <span className="text-nano text-fg-muted uppercase">Classic</span>
            <IPTag address="tg://proxy?server=185.76.151.1&port=443&secret=ee..." />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-nano text-fg-muted uppercase">Secure</span>
            <IPTag address="tg://proxy?server=185.76.151.1&port=443&secret=dd..." />
          </div>
        </div>
      </PopoverContent>
    </Popover>
  ),
};
