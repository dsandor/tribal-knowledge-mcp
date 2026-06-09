import React from 'react'
import TextField, { TextFieldProps } from '@mui/material/TextField'

// Extended props that accept HTML input attributes and forward them via slotProps
type InputProps = Omit<TextFieldProps, 'variant'> & {
  className?: string
  // HTML input attributes that aren't standard TextFieldProps
  step?: string | number
  min?: string | number
  max?: string | number
}

export function Input({ className, step, min, max, ...props }: InputProps) {
  const nativeInputProps: Record<string, unknown> = {}
  if (step !== undefined) nativeInputProps.step = step
  if (min !== undefined) nativeInputProps.min = min
  if (max !== undefined) nativeInputProps.max = max

  const extraSlotProps = Object.keys(nativeInputProps).length > 0
    ? { htmlInput: nativeInputProps }
    : undefined

  return (
    <TextField
      variant="outlined"
      size="small"
      fullWidth
      slotProps={extraSlotProps}
      {...props}
    />
  )
}
