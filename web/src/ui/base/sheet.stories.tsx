import type { Meta, StoryObj } from "@storybook/react";
import { Sheet, SheetTrigger, SheetContent, SheetHeader, SheetTitle, SheetBody } from "./sheet";
import { Button } from "./button";
import { StatusBeacon } from "@/ui/primitives/StatusBeacon";

const meta: Meta = { title: "UI/Sheet", parameters: { layout: "fullscreen" } };
export default meta;
type Story = StoryObj;

export const RightSheet: Story = {
  render: () => (
    <div className="p-8">
      <Sheet>
        <SheetTrigger asChild>
          <Button>Node Details</Button>
        </SheetTrigger>
        <SheetContent side="right">
          <SheetHeader>
            <div className="flex items-center gap-2">
              <StatusBeacon status="ok" size="sm" />
              <SheetTitle>node-eu-fra-01</SheetTitle>
            </div>
          </SheetHeader>
          <SheetBody>
            <div className="grid grid-cols-2 gap-2">
              {[
                { value: "42%", label: "CPU" },
                { value: "6.1 GB", label: "Memory" },
                { value: "14d 7h", label: "Uptime" },
                { value: "99.98%", label: "Health" },
              ].map((m) => (
                <div key={m.label} className="flex flex-col gap-1">
                  <span className="text-lg font-mono text-fg">{m.value}</span>
                  <span className="text-nano text-fg-muted uppercase">{m.label}</span>
                </div>
              ))}
            </div>
          </SheetBody>
        </SheetContent>
      </Sheet>
    </div>
  ),
};

export const BottomSheet: Story = {
  render: () => (
    <div className="p-8">
      <Sheet>
        <SheetTrigger asChild>
          <Button variant="outline">Quick Actions</Button>
        </SheetTrigger>
        <SheetContent side="bottom">
          <SheetHeader>
            <SheetTitle>Actions</SheetTitle>
          </SheetHeader>
          <SheetBody>
            <div className="flex flex-col gap-2">
              <Button variant="ghost" className="justify-start">
                ⟳ Restart Node
              </Button>
              <Button variant="ghost" className="justify-start">
                ⚙ Reconfigure
              </Button>
              <Button variant="ghost" className="justify-start">
                📋 View Logs
              </Button>
              <Button variant="ghost" className="justify-start text-status-error">
                ⏻ Force Stop
              </Button>
            </div>
          </SheetBody>
        </SheetContent>
      </Sheet>
    </div>
  ),
};
