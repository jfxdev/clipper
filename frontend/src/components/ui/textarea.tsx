import * as React from "react"

import { cn } from "@/lib/utils"

// Browser and extension text services are a real exfiltration path for
// anything typed here: Edge's "enhanced" spellcheck and Grammarly both ship
// field contents to their own servers for analysis. For a box whose entire
// purpose is holding a secret before it is encrypted, all of that has to be
// off — client-side encryption is worth nothing if the plaintext was
// already uploaded by the browser on the way in.
function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      spellCheck={false}
      autoComplete="off"
      autoCorrect="off"
      autoCapitalize="off"
      data-gramm="false"
      data-gramm_editor="false"
      data-enable-grammarly="false"
      className={cn(
        "border-input placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 aria-invalid:border-destructive flex field-sizing-content min-h-16 w-full rounded-md border bg-transparent px-3 py-2 text-base shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
