import React from 'react'
import Chip, { ChipProps } from '@mui/material/Chip'

type ChipColor = NonNullable<ChipProps['color']>

const variantColorMap: Record<string, ChipColor> = {
  draft: 'warning',
  published: 'success',
  default: 'default',
}

interface BadgeProps extends Omit<ChipProps, 'variant' | 'children'> {
  variant?: string
  children?: React.ReactNode
}

export function Badge({ variant, children, ...props }: BadgeProps) {
  const color: ChipColor = (variant && variantColorMap[variant]) || 'default'
  return (
    <Chip
      label={children}
      size="small"
      color={color}
      variant="outlined"
      {...props}
    />
  )
}
