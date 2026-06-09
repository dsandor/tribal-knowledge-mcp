import React from 'react'
import MuiCard from '@mui/material/Card'
import MuiCardContent from '@mui/material/CardContent'
import MuiCardHeader from '@mui/material/CardHeader'
import Typography from '@mui/material/Typography'

export function Card({ children, className, onClick, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <MuiCard onClick={onClick} {...props}>{children}</MuiCard>
}

export function CardContent({ children, className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <MuiCardContent {...props}>{children}</MuiCardContent>
}

export function CardHeader({ children, className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <MuiCardHeader subheader={children} {...(props as any)} />
}

export function CardTitle({ children, className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return (
    <Typography variant="subtitle2" sx={{ fontWeight: 600 }} {...(props as any)}>
      {children}
    </Typography>
  )
}
