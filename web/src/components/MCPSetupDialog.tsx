import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControl from '@mui/material/FormControl'
import IconButton from '@mui/material/IconButton'
import InputLabel from '@mui/material/InputLabel'
import Link from '@mui/material/Link'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { Check, Copy, X } from 'lucide-react'
import { getMCPInfo, type APIKey, type MCPInfo } from '@/lib/api'
import { copyToClipboard } from '@/lib/clipboard'

interface Props {
  open: boolean
  onClose: () => void
  keys: APIKey[]
  meUserId?: string
  canManageKeys?: boolean
  onCopied: (ok: boolean) => void
}

// Dark code block with a copy button in the top-right corner.
function CodeSnippet({ code, onCopied }: { code: string; onCopied: (ok: boolean) => void }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    const ok = await copyToClipboard(code)
    onCopied(ok)
    if (ok) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <Box sx={{ position: 'relative', mt: 1 }}>
      <Box
        component="pre"
        sx={{
          m: 0,
          p: 1.5,
          pr: 5,
          bgcolor: 'rgba(0,0,0,0.35)',
          border: '1px solid',
          borderColor: 'divider',
          borderRadius: 1,
          fontFamily: 'monospace',
          fontSize: 12.5,
          lineHeight: 1.6,
          overflowX: 'auto',
          whiteSpace: 'pre',
        }}
      >
        {code}
      </Box>
      <Tooltip title={copied ? 'Copied' : 'Copy to clipboard'}>
        <IconButton
          size="small"
          onClick={handleCopy}
          sx={{ position: 'absolute', top: 6, right: 6, color: copied ? 'primary.main' : 'text.secondary' }}
        >
          {copied ? <Check size={15} /> : <Copy size={15} />}
        </IconButton>
      </Tooltip>
    </Box>
  )
}

export default function MCPSetupDialog({ open, onClose, keys, meUserId, canManageKeys, onCopied }: Props) {
  const navigate = useNavigate()
  const [tab, setTab] = useState(0)
  const [info, setInfo] = useState<MCPInfo | null>(null)
  const [infoFailed, setInfoFailed] = useState(false)
  const [selectedKeyId, setSelectedKeyId] = useState<string>('')

  // Keys usable in snippets: must have a retained plaintext value.
  // The caller's personal keys come first, then team keys.
  const available = useMemo(() => {
    const usable = keys.filter((k) => k.raw_key)
    const mine = usable.filter((k) => k.key_type === 'user' && k.user_id === meUserId)
    const team = usable.filter((k) => k.key_type === 'team')
    return [...mine, ...team]
  }, [keys, meUserId])

  useEffect(() => {
    if (!open) return
    setInfoFailed(false)
    getMCPInfo()
      .then(setInfo)
      .catch(() => { setInfo(null); setInfoFailed(true) })
  }, [open])

  // Default to the first available key whenever the list changes.
  useEffect(() => {
    if (available.length > 0 && !available.some((k) => k.id === selectedKeyId)) {
      setSelectedKeyId(available[0].id)
    }
  }, [available, selectedKeyId])

  const selected = available.find((k) => k.id === selectedKeyId)
  const serverURL = info?.url || '<server-url>'
  const keyValue = selected?.raw_key || '<your-api-key>'

  const claudeCodeSnippet =
    `claude mcp add --transport http tribal-knowledge \\\n` +
    `  ${serverURL} \\\n` +
    `  --header "Authorization: Bearer ${keyValue}"`

  const desktopSnippet = `{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "${serverURL}", "--header", "Authorization:\${TK_AUTH}"],
      "env": { "TK_AUTH": "Bearer ${keyValue}" }
    }
  }
}`

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', pr: 1 }}>
        Connect an MCP Client
        <IconButton size="small" onClick={onClose}><X size={16} /></IconButton>
      </DialogTitle>
      <DialogContent>
        {info && !info.http_enabled && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            Remote MCP is not enabled on this server. Set <code>MCP_HTTP_ADDR</code> (e.g. <code>:8081</code>) and
            restart the server, then reopen this dialog.
          </Alert>
        )}
        {infoFailed && (
          <Alert severity="info" sx={{ mb: 2 }}>
            Could not determine the server&apos;s MCP URL — replace <code>&lt;server-url&gt;</code> below with your
            server&apos;s MCP endpoint.
          </Alert>
        )}
        {available.length === 0 && (
          <Alert severity="info" sx={{ mb: 2 }}>
            You have no copyable API keys.{' '}
            {canManageKeys ? (
              <>
                <Link component="button" onClick={() => { onClose(); navigate('/api-keys') }} sx={{ verticalAlign: 'baseline' }}>
                  Create one on the API Keys page
                </Link>{' '}
                and reopen this dialog, or replace <code>&lt;your-api-key&gt;</code> below.
              </>
            ) : (
              <>
                Ask a team admin to create an API key for you, or replace <code>&lt;your-api-key&gt;</code> below.
              </>
            )}
          </Alert>
        )}

        {available.length > 1 && (
          <FormControl size="small" fullWidth sx={{ mb: 1.5 }}>
            <InputLabel id="mcp-key-select-label">API key</InputLabel>
            <Select
              labelId="mcp-key-select-label"
              label="API key"
              value={selectedKeyId}
              onChange={(e) => setSelectedKeyId(e.target.value)}
            >
              {available.map((k) => (
                <MenuItem key={k.id} value={k.id}>
                  {k.name} ({k.key_type})
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ mb: 1, minHeight: 36 }}>
          <Tab label="Claude Code" sx={{ minHeight: 36, textTransform: 'none' }} />
          <Tab label="Claude Desktop" sx={{ minHeight: 36, textTransform: 'none' }} />
        </Tabs>

        {tab === 0 && (
          <Box>
            <Typography variant="body2" color="text.secondary">
              Run this in your terminal to register the server with Claude Code:
            </Typography>
            <CodeSnippet code={claudeCodeSnippet} onCopied={onCopied} />
          </Box>
        )}
        {tab === 1 && (
          <Box>
            <Typography variant="body2" color="text.secondary">
              Add this to your Claude Desktop config file, then restart Claude Desktop:
            </Typography>
            <Typography variant="caption" component="div" sx={{ mt: 0.5, color: 'text.secondary', fontFamily: 'monospace', fontSize: 11 }}>
              macOS: ~/Library/Application Support/Claude/claude_desktop_config.json
              <br />
              Windows: %APPDATA%\Claude\claude_desktop_config.json
            </Typography>
            <CodeSnippet code={desktopSnippet} onCopied={onCopied} />
            <Typography variant="caption" sx={{ display: 'block', mt: 1, color: 'text.secondary' }}>
              Requires Node.js — the <code>npx mcp-remote</code> bridge connects Claude Desktop to the remote server.
            </Typography>
          </Box>
        )}

        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2 }}>
          <Button size="small" onClick={onClose}>Close</Button>
        </Box>
      </DialogContent>
    </Dialog>
  )
}
