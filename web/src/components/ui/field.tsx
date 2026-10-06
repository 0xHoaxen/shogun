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
          "min-h-10 w-full border border-ink bg-field px-2.5 py-2 text-xs text-ink",
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
          "min-h-24 w-full border border-ink bg-field px-2.5 py-2 text-xs leading-relaxed text-ink",
          className,
        )}
        {...textareaProps}
      />
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
      <select
        id={fieldId}
        className={cn("min-h-10 w-full border border-ink bg-field px-2.5 py-2 text-xs text-ink", className)}
        {...selectProps}
      >
        {children}
      </select>
    </div>
  );
}
