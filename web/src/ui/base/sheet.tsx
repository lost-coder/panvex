import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as VisuallyHidden from "@radix-ui/react-visually-hidden";
import { Sheet as ModalSheet } from "react-modal-sheet";
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

// react-modal-sheet handles bottom and fullScreen variants (native swipe physics)
const isMobileVariant = (side: string, fullScreen: boolean) => fullScreen || side === "bottom";

const SheetContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  SheetContentProps
>(
  (
    { side = "right", fullScreen = false, title, onOpenChange, className, children, ...props },
    ref,
  ) => {
    const useModalSheet = isMobileVariant(side, fullScreen);
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
      typeof import.meta !== "undefined" &&
      (import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV
    ) {
      // eslint-disable-next-line no-console
      console.warn(
        "[panvex-ui] <SheetContent> needs an accessible name — either render a <SheetTitle> inside children or pass the `title` prop. Radix Dialog will otherwise log a missing-title warning.",
      );
    }

    const requestClose = () => onOpenChange?.(false);

    if (useModalSheet) {
      return (
        <DialogPrimitive.Portal>
          <DialogPrimitive.Content ref={ref} asChild {...props}>
            <div className="fixed inset-0 z-50 pointer-events-none">
              <ModalSheet
                isOpen={true}
                onClose={requestClose}
                detent={fullScreen ? "full" : "content"}
                style={{ pointerEvents: "auto" }}
              >
                <ModalSheet.Backdrop onTap={requestClose} />
                <ModalSheet.Container
                  className={cn(
                    "!bg-bg-card border border-border shadow-xl",
                    fullScreen ? "!rounded-none" : "!rounded-t-xl",
                  )}
                >
                  <ModalSheet.Header className="pt-2 pb-0 flex justify-center w-full">
                    <ModalSheet.DragIndicator className="!mt-2 !mx-auto" />
                  </ModalSheet.Header>
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
                  <ModalSheet.Content className={cn("flex flex-col", className)}>
                    {children}
                  </ModalSheet.Content>
                </ModalSheet.Container>
              </ModalSheet>
            </div>
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      );
    }

    // Standard Radix Dialog for side panels (left/right) with slide animation
    return (
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 duration-300" />
        <DialogPrimitive.Content
          ref={ref}
          className={cn(
            "fixed z-50 bg-bg-card border-border shadow-xl flex flex-col overflow-y-auto",
            "duration-300 data-[state=open]:animate-in data-[state=closed]:animate-out",
            side === "right"
              ? "inset-y-0 right-0 h-full w-[320px] border-l data-[state=open]:slide-in-from-right data-[state=closed]:slide-out-to-right"
              : "inset-y-0 left-0 h-full w-[320px] border-r data-[state=open]:slide-in-from-left data-[state=closed]:slide-out-to-left",
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

function SheetTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("text-base font-semibold text-fg", className)} {...props} />;
}

function SheetBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex-1 px-5 py-4 overflow-y-auto", className)} {...props} />;
}

export { Sheet, SheetTrigger, SheetClose, SheetContent, SheetHeader, SheetTitle, SheetBody };
