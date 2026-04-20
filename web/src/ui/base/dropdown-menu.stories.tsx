import type { Meta, StoryObj } from "@storybook/react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "./dropdown-menu";
import { Button } from "./button";

const meta: Meta = { title: "UI/DropdownMenu" };
export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          ⋮
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>⟳ Restart</DropdownMenuItem>
        <DropdownMenuItem>⚙ Configure</DropdownMenuItem>
        <DropdownMenuItem>📋 View Logs</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem>🔑 Rotate Secret</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem danger>⏻ Force Stop</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
};
