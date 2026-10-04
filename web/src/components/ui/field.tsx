import { useId, type InputHTMLAttributes } from "react";

import { cn } from "@/lib/utils";

interface TextFieldProps extends InputHTMLAttributes<HTMLInputElement> {
  label: string;
}

// A labelled text input: uppercase mono label above a ruled white field.
export function TextField({ label, id, className, ...inputProps }: TextFieldProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  return (
    <div>
      <label htmlFor={fieldId} className="label mb-1.5 block text-[11px] font-medium">
        {label}
      </label>
      <input
        id={fieldId}
        className={cn(
          "min-h-10 w-full border border-ink bg-field px-2.5 py-2 text-xs text-ink",
          className,
        )}
        {...inputProps}
      />
    </div>
  );
}
