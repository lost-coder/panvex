import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as VisuallyHidden from "@radix-ui/react-visually-hidden";
import { cn } from "@/ui/lib/cn";

const Sheet = DialogPrimitive.Root;
const SheetTrigger = DialogPrimitive.Trigger;
const SheetClose = DialogPrimitive.Close;

interface SheetContentProps extends React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> {
  side?: "left" | "right" | "bottom";
  fullScreen?: boolean;
  /**
   * Accessible title announced to screen readers when no visible SheetTitle
   * is rendered inside `children`. P2-FE-07 / M-F6: required for a11y — the
   * previous generic "Sheet" fallback produced a useless announcement.
   * Either render a visible <SheetTitle> inside the sheet or pass a
   * meaningful string here that describes the sheet's purpose.
   */
  title?: string;
  /** Callback to close the sheet — thread from Sheet (DialogPrimitive.Root) onOpenChange. */
  onOpenChange?: (open: boolean) => void;
}

const SheetContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  SheetContentProps
>(
  (
    { side = "right", fullScreen = false, title, onOpenChange: _onOpenChange, className, children, ...props },
    ref,
  ) => {
    // Unused prop kept for backward compatibility with existing callers
    // that pass onOpenChange on SheetContent itself. Close is routed via
    // the parent <Sheet>'s onOpenChange — this handler is effectively a
    // no-op.
    // P2-FE-07 / M-F6: removed the generic "Sheet" default. If the caller
    // renders a visible <SheetTitle> inside children, Radix picks that up
    // and no hidden title is needed. Only fall back to a hidden title when
    // the caller passes one explicitly — in dev, warn if neither path is
    // taken so the missing accessible name surfaces during development.
    const hasExplicitTitle = typeof title === "string" && title.length > 0;
    // P2-FE-07 / M-F6: surface missing accessible-name mistakes early.
    // `import.meta.env.DEV` is Vite's compile-time dev flag; it's `true`
    // during `vite dev` / Storybook / vitest and `false` in production
    // bundles, so the warn is free of runtime cost in consumers.
    if (
      !hasExplicitTitle &&
      import.meta !== undefined &&
      (import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV
    ) {
      console.warn(
        "[panvex-ui] <SheetContent> needs an accessible name — either render a <SheetTitle> inside children or pass the `title` prop. Radix Dialog will otherwise log a missing-title warning.",
      );
    }

    // Radix handles open/close itself via the parent <Sheet>
    // (DialogPrimitive.Root) onOpenChange, so SheetContent doesn't need
    // a manual dismiss path anymore. Kept the onOpenChange prop in the
    // signature to avoid churning every call site.

    // Previously `side="bottom"` was handled by react-modal-sheet,
    // whose Framer Motion drag recognizer captured pointerdown on
    // every child — text inputs inside the sheet could not receive
    // focus or accept keystrokes even with `disableDrag`. We now
    // render every side variant through Radix Dialog.Content and
    // style the bottom variant as a sheet ourselves. Swipe-to-
    // dismiss is gone; dismiss happens via backdrop tap, Cancel
    // button, or Escape key.
    const sideClass = (() => {
      if (side === "bottom") {
        return cn(
          "inset-x-0 bottom-0 w-full border-t rounded-t-xl",
          fullScreen ? "top-0 rounded-none" : "max-h-[85vh]",
          "data-[state=open]:slide-in-from-bottom data-[state=closed]:slide-out-to-bottom",
        );
      }
      if (side === "right") {
        return "inset-y-0 right-0 h-full w-[320px] border-l data-[state=open]:slide-in-from-right data-[state=closed]:slide-out-to-right";
      }
      return "inset-y-0 left-0 h-full w-[320px] border-r data-[state=open]:slide-in-from-left data-[state=closed]:slide-out-to-left";
    })();

    return (
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 duration-300" />
        <DialogPrimitive.Content
          ref={ref}
          className={cn(
            "fixed z-50 bg-bg-card border-border shadow-xl flex flex-col overflow-y-auto",
            "duration-300 data-[state=open]:animate-in data-[state=closed]:animate-out",
            sideClass,
            className,
          )}
          {...props}
        >
          {hasExplicitTitle && (
            <>
              <VisuallyHidden.Root asChild>
                <DialogPrimitive.Title>{title}</DialogPrimitive.Title>
              </VisuallyHidden.Root>
              <VisuallyHidden.Root asChild>
                <DialogPrimitive.Description>{title} content</DialogPrimitive.Description>
              </VisuallyHidden.Root>
            </>
          )}
          {children}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    );
  },
);
SheetContent.displayName = DialogPrimitive.Content.displayName;

function SheetHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex flex-col gap-1 px-5 py-4 border-b border-border", className)}
      {...props}
    />
  );
}

function SheetTitle({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2 className={cn("text-base font-semibold text-fg", className)} {...props}>
      {children}
    </h2>
  );
}

function SheetBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex-1 px-5 py-4 overflow-y-auto", className)} {...props} />;
}

export { Sheet, SheetTrigger, SheetClose, SheetContent, SheetHeader, SheetTitle, SheetBody };
