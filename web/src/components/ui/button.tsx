import React from 'react'
import MuiButton, { ButtonProps as MuiButtonProps } from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'

type ShadcnVariant = 'default' | 'outline' | 'ghost' | 'destructive' | 'success' | string
type ShadcnSize = 'default' | 'sm' | 'lg' | 'icon'

interface ButtonProps extends Omit<MuiButtonProps, 'variant' | 'size'> {
  variant?: ShadcnVariant
  size?: ShadcnSize | MuiButtonProps['size']
  asChild?: boolean
}

export function Button({ variant = 'default', size = 'default', children, asChild: _asChild, ...props }: ButtonProps) {
  const muiVariant: MuiButtonProps['variant'] =
    variant === 'outline' ? 'outlined' :
    variant === 'ghost' ? 'text' :
    'contained'

  const muiColor: MuiButtonProps['color'] =
    variant === 'destructive' ? 'error' :
    variant === 'success' ? 'success' :
    'primary'

  if (size === 'icon') {
    return (
      <IconButton color={muiColor} {...(props as any)}>
        {children}
      </IconButton>
    )
  }

  const muiSize: MuiButtonProps['size'] =
    size === 'sm' ? 'small' :
    size === 'lg' ? 'large' :
    'medium'

  return (
    <MuiButton variant={muiVariant} color={muiColor} size={muiSize} {...props}>
      {children}
    </MuiButton>
  )
}
