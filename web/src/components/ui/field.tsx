import {
  useId,
  type InputHTMLAttributes,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";

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
          "min-h-10 w-full border border-line2 bg-bg px-2.5 py-2 text-xs text-text",
          className,
        )}
        {...inputProps}
      />
    </div>
  );
}

interface TextAreaFieldProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label: string;
}

// A labelled multi-line input, styled like TextField.
export function TextAreaField({ label, id, className, ...textareaProps }: TextAreaFieldProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  return (
    <div>
      <label htmlFor={fieldId} className="label mb-1.5 block text-[11px] font-medium">
        {label}
      </label>
      <textarea
        id={fieldId}
        className={cn(
          "min-h-24 w-full border border-line2 bg-bg px-2.5 py-2 text-xs leading-relaxed text-text",
          className,
        )}
        {...textareaProps}
      />
    </div>
  );
}

// A native select with the platform arrow replaced by a ruled chevron, so it
// sits inside the field's padding instead of against the border.
export function Select({ className, children, ...selectProps }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className={cn("relative", className)}>
      <select
        className="min-h-10 w-full cursor-pointer appearance-none border border-line2 bg-bg py-2 pr-9 pl-2.5 text-xs text-text disabled:cursor-not-allowed disabled:text-muted-foreground"
        {...selectProps}
      >
        {children}
      </select>
      <svg
        aria-hidden="true"
        viewBox="0 0 10 6"
        className="pointer-events-none absolute top-1/2 right-3 h-1.5 w-2.5 -translate-y-1/2 text-text"
      >
        <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" />
      </svg>
    </div>
  );
}

interface SelectFieldProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label: string;
}

// A labelled native select, styled like TextField.
export function SelectField({ label, id, className, children, ...selectProps }: SelectFieldProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  return (
    <div>
      <label htmlFor={fieldId} className="label mb-1.5 block text-[11px] font-medium">
        {label}
      </label>
      <Select id={fieldId} className={className} {...selectProps}>
        {children}
      </Select>
    </div>
  );
}
