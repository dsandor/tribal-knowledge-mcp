import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import IconButton from '@mui/material/IconButton'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Snackbar from '@mui/material/Snackbar'
import { Copy, Check, Eye, Code } from 'lucide-react'

type ViewMode = 'rendered' | 'raw'

/**
 * Copy text to the clipboard, falling back to execCommand for non-HTTPS
 * contexts where navigator.clipboard is unavailable (mirrors APIKeys.tsx).
 */
async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    return
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}

// Styling applied to the rendered markdown tree so it fits the dark MUI theme.
const renderedSx = {
  color: 'text.secondary',
  lineHeight: 1.6,
  fontSize: '0.875rem',
  '& :first-of-type': { mt: 0 },
  '& :last-child': { mb: 0 },
  '& h1, & h2, & h3, & h4, & h5, & h6': {
    color: 'text.primary',
    fontWeight: 600,
    lineHeight: 1.3,
    mt: 2,
    mb: 1,
  },
  '& h1': { fontSize: '1.5rem' },
  '& h2': { fontSize: '1.25rem' },
  '& h3': { fontSize: '1.1rem' },
  '& h4, & h5, & h6': { fontSize: '1rem' },
  '& p': { my: 1 },
  '& a': { color: 'primary.light', textDecoration: 'none', '&:hover': { textDecoration: 'underline' } },
  '& ul, & ol': { my: 1, pl: 3 },
  '& li': { my: 0.5 },
  '& code': {
    fontFamily: 'monospace',
    fontSize: '0.85em',
    backgroundColor: 'rgba(255,255,255,0.08)',
    px: 0.5,
    py: 0.25,
    borderRadius: 1,
  },
  '& pre': {
    backgroundColor: 'rgba(0,0,0,0.3)',
    border: '1px solid rgba(255,255,255,0.08)',
    borderRadius: 1,
    p: 1.5,
    overflowX: 'auto',
    my: 1.5,
  },
  '& pre code': { backgroundColor: 'transparent', p: 0, fontSize: '0.85rem' },
  '& blockquote': {
    borderLeft: '3px solid',
    borderColor: 'primary.main',
    pl: 2,
    ml: 0,
    my: 1.5,
    color: 'text.secondary',
    fontStyle: 'italic',
  },
  '& table': { borderCollapse: 'collapse', my: 1.5, display: 'block', overflowX: 'auto' },
  '& th, & td': { border: '1px solid rgba(255,255,255,0.12)', px: 1, py: 0.5, textAlign: 'left' },
  '& th': { backgroundColor: 'rgba(255,255,255,0.04)', fontWeight: 600 },
  '& hr': { border: 'none', borderTop: '1px solid rgba(255,255,255,0.08)', my: 2 },
  '& img': { maxWidth: '100%' },
} as const

interface MarkdownViewProps {
  content: string
}

/**
 * Renders markdown content with a toggle between a rendered reader view and the
 * raw markdown source, plus a button to copy the raw markdown to the clipboard.
 */
export function MarkdownView({ content }: MarkdownViewProps) {
  const [mode, setMode] = useState<ViewMode>('rendered')
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await copyToClipboard(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 1, mb: 1 }}>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={mode}
          onChange={(_, next: ViewMode | null) => { if (next) setMode(next) }}
        >
          <ToggleButton value="rendered" sx={{ textTransform: 'none', gap: 0.5, px: 1.25 }}>
            <Eye style={{ width: 14, height: 14 }} />
            Rendered
          </ToggleButton>
          <ToggleButton value="raw" sx={{ textTransform: 'none', gap: 0.5, px: 1.25 }}>
            <Code style={{ width: 14, height: 14 }} />
            Raw
          </ToggleButton>
        </ToggleButtonGroup>
        <IconButton size="small" onClick={handleCopy} title="Copy raw markdown">
          {copied
            ? <Check style={{ width: 16, height: 16, color: '#10b981' }} />
            : <Copy style={{ width: 16, height: 16 }} />}
        </IconButton>
      </Box>

      {mode === 'rendered' ? (
        <Box sx={renderedSx}>
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
        </Box>
      ) : (
        <Typography
          component="pre"
          variant="body2"
          color="text.secondary"
          sx={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', lineHeight: 1.6, m: 0 }}
        >
          {content}
        </Typography>
      )}

      <Snackbar
        open={copied}
        message="Markdown copied to clipboard"
        autoHideDuration={2000}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      />
    </Box>
  )
}
