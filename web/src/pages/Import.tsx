import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Upload, FileText, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { importKnowledge, importKnowledgeCSV, type ImportResult, type KnowledgeEntry } from '@/lib/api'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import Button from '@mui/material/Button'
import Tabs from '@mui/material/Tabs'
import Tab from '@mui/material/Tab'
import TextField from '@mui/material/TextField'
import Alert from '@mui/material/Alert'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableHead from '@mui/material/TableHead'
import TableBody from '@mui/material/TableBody'
import TableRow from '@mui/material/TableRow'
import TableCell from '@mui/material/TableCell'
import TableContainer from '@mui/material/TableContainer'
import CircularProgress from '@mui/material/CircularProgress'

type Tab = 'json' | 'csv'

const EXAMPLE_JSON = `[
  {
    "title": "My best prompt",
    "content": "Always start with context...",
    "type": "prompt_template",
    "domain": "general",
    "tags": ["prompts"]
  }
]`

function ResultBox({ result }: { result: ImportResult }) {
  const [showErrors, setShowErrors] = useState(false)
  const hasErrors = result.errors && result.errors.length > 0

  return (
    <Alert
      severity="success"
      icon={<CheckCircle size={16} />}
      sx={{ '& .MuiAlert-message': { width: '100%' } }}
    >
      <Box>
        <Typography variant="body2" sx={{ fontWeight: 500 }}>
          Imported {result.imported} {result.imported === 1 ? 'entry' : 'entries'}
          {result.skipped > 0 && (
            <Typography component="span" variant="body2" color="text.secondary" sx={{ fontWeight: 400 }}>
              {' '}&middot;{' '}Skipped {result.skipped} {result.skipped === 1 ? 'duplicate' : 'duplicates'}
            </Typography>
          )}
        </Typography>

        {hasErrors && (
          <Box sx={{ mt: 1 }}>
            <Button
              variant="text"
              size="small"
              onClick={() => setShowErrors(v => !v)}
              sx={{ p: 0, fontSize: 13, color: 'warning.main', textDecoration: 'underline', minWidth: 0 }}
            >
              {showErrors ? 'Hide' : 'Show'} {result.errors.length} {result.errors.length === 1 ? 'error' : 'errors'}
            </Button>
            {showErrors && (
              <Box
                component="ul"
                sx={{
                  mt: 1,
                  p: 1.5,
                  bgcolor: 'background.default',
                  borderRadius: 1,
                  maxHeight: 160,
                  overflowY: 'auto',
                  listStyle: 'none',
                  m: 0,
                  fontFamily: 'monospace',
                  fontSize: 12,
                  color: 'error.light',
                }}
              >
                {result.errors.map((e, i) => (
                  <li key={i}>{e}</li>
                ))}
              </Box>
            )}
          </Box>
        )}

        <Box sx={{ mt: 1 }}>
          <Link
            to="/knowledge"
            style={{ fontSize: 14, color: '#10b981', textDecoration: 'underline' }}
          >
            Go to Knowledge Browser &rarr;
          </Link>
        </Box>
      </Box>
    </Alert>
  )
}

function JsonTab() {
  const [raw, setRaw] = useState('')
  const [parseError, setParseError] = useState<string | null>(null)
  const [parsed, setParsed] = useState<Partial<KnowledgeEntry>[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [importError, setImportError] = useState<string | null>(null)

  function handlePreview() {
    setParseError(null)
    setParsed(null)
    setResult(null)
    setImportError(null)
    try {
      const val = JSON.parse(raw)
      if (!Array.isArray(val)) throw new Error('Expected a JSON array at the top level')
      setParsed(val)
    } catch (e) {
      setParseError(e instanceof Error ? e.message : String(e))
    }
  }

  async function handleImport() {
    if (!parsed) return
    setLoading(true)
    setResult(null)
    setImportError(null)
    try {
      const res = await importKnowledge(parsed)
      setResult(res)
    } catch (e) {
      setImportError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  const previewRows = parsed ? parsed.slice(0, 10) : []

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      <Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          Paste a JSON array of knowledge entries
        </Typography>
        <TextField
          fullWidth
          multiline
          minRows={8}
          value={raw}
          onChange={e => { setRaw(e.target.value); setParsed(null); setParseError(null); setResult(null) }}
          placeholder={'[\n  {"title": "...", "content": "...", "type": "...", "domain": "...", "tags": []}\n]'}
          error={!!parseError}
          slotProps={{ htmlInput: { spellCheck: false } }}
          sx={{
            '& textarea': { fontFamily: 'monospace', fontSize: 13 },
          }}
        />
        {parseError && (
          <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, mt: 0.5, color: 'error.main' }}>
            <AlertCircle size={16} style={{ marginTop: 2, flexShrink: 0 }} />
            <Typography variant="body2" color="error">{parseError}</Typography>
          </Box>
        )}
      </Box>

      <Box component="details">
        <Box component="summary" sx={{ cursor: 'pointer', fontSize: 12, color: 'text.secondary', userSelect: 'none' }}>
          Show example JSON
        </Box>
        <Box
          component="pre"
          sx={{
            mt: 1,
            p: 1.5,
            bgcolor: 'background.default',
            border: '1px solid',
            borderColor: 'divider',
            borderRadius: 1,
            fontSize: 12,
            fontFamily: 'monospace',
            color: 'text.secondary',
            overflowX: 'auto',
            whiteSpace: 'pre-wrap',
          }}
        >
          {EXAMPLE_JSON}
        </Box>
      </Box>

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button variant="outlined" onClick={handlePreview} disabled={!raw.trim()}>
          Preview
        </Button>
        <Button
          variant="contained"
          onClick={handleImport}
          disabled={!parsed || loading}
          startIcon={loading ? <CircularProgress size={14} color="inherit" /> : undefined}
        >
          {loading
            ? 'Importing...'
            : `Import ${parsed ? parsed.length : 0} ${parsed && parsed.length === 1 ? 'entry' : 'entries'}`
          }
        </Button>
      </Box>

      {parsed && !result && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ mb: 1, display: 'block' }}>
            Showing {previewRows.length} of {parsed.length} {parsed.length === 1 ? 'entry' : 'entries'}
          </Typography>
          <TableContainer component={Paper}>
            <Table size="small">
              <TableHead>
                <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)', fontSize: 12 } }}>
                  <TableCell>Title</TableCell>
                  <TableCell>Type</TableCell>
                  <TableCell>Domain</TableCell>
                  <TableCell>Tags</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {previewRows.map((row, i) => (
                  <TableRow key={i} hover sx={{ '& td': { fontSize: 12 } }}>
                    <TableCell sx={{ maxWidth: 200 }}>
                      <Typography variant="caption" noWrap sx={{ display: 'block' }}>
                        {(row as any).title ?? <Typography component="span" variant="caption" color="text.disabled">—</Typography>}
                      </Typography>
                    </TableCell>
                    <TableCell>{(row as any).type ?? <Typography variant="caption" color="text.disabled">—</Typography>}</TableCell>
                    <TableCell>{(row as any).domain ?? <Typography variant="caption" color="text.disabled">—</Typography>}</TableCell>
                    <TableCell sx={{ maxWidth: 160 }}>
                      <Typography variant="caption" noWrap sx={{ display: 'block' }}>
                        {Array.isArray((row as any).tags)
                          ? (row as any).tags.join(', ')
                          : ((row as any).tags ?? <Typography component="span" variant="caption" color="text.disabled">—</Typography>)}
                      </Typography>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Box>
      )}

      {importError && (
        <Alert severity="error" icon={<AlertCircle size={16} />}>
          {importError}
        </Alert>
      )}

      {result && <ResultBox result={result} />}
    </Box>
  )
}

function CsvTab() {
  const inputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [importError, setImportError] = useState<string | null>(null)
  const [rowEstimate, setRowEstimate] = useState<number | null>(null)

  function acceptFile(f: File) {
    if (!f.name.endsWith('.csv')) return
    setFile(f)
    setResult(null)
    setImportError(null)
    // Count newlines to estimate rows
    const reader = new FileReader()
    reader.onload = e => {
      const text = (e.target?.result as string) ?? ''
      const lines = text.split('\n').filter(l => l.trim().length > 0)
      setRowEstimate(Math.max(0, lines.length - 1)) // subtract header
    }
    reader.readAsText(f)
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault()
    setDragOver(false)
    const f = e.dataTransfer.files[0]
    if (f) acceptFile(f)
  }

  function handleFileInput(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    if (f) acceptFile(f)
  }

  async function handleUpload() {
    if (!file) return
    setLoading(true)
    setResult(null)
    setImportError(null)
    try {
      const res = await importKnowledgeCSV(file)
      setResult(res)
    } catch (e) {
      setImportError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      <Box>
        <Typography variant="caption" color="text.secondary" sx={{ mb: 1, display: 'block' }}>
          CSV header must be:{' '}
          <Box component="code" sx={{ fontFamily: 'monospace', bgcolor: 'background.default', px: 0.75, borderRadius: 0.5, fontSize: 12 }}>
            title,content,type,domain,tags
          </Box>
        </Typography>
        <Box
          onClick={() => inputRef.current?.click()}
          onDragOver={e => { e.preventDefault(); setDragOver(true) }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          sx={{
            border: '2px dashed',
            borderColor: dragOver ? 'primary.main' : 'divider',
            borderRadius: 2,
            p: 4,
            textAlign: 'center',
            cursor: 'pointer',
            bgcolor: dragOver ? 'rgba(16,185,129,0.05)' : 'background.paper',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 1.5,
            minHeight: 160,
            justifyContent: 'center',
            transition: 'border-color 0.15s, background-color 0.15s',
            userSelect: 'none',
          }}
        >
          <Upload size={32} style={{ color: dragOver ? '#10b981' : '#64748b' }} />
          <Typography variant="body2" color="text.secondary">Drop a CSV file here or click to browse</Typography>
          <Typography variant="caption" color="text.disabled">.csv files only</Typography>
        </Box>
        <input
          ref={inputRef}
          type="file"
          accept=".csv"
          style={{ display: 'none' }}
          onChange={handleFileInput}
        />
      </Box>

      {file && (
        <Paper sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 2, py: 1, border: '1px solid', borderColor: 'divider' }}>
          <FileText size={16} style={{ color: '#94a3b8', flexShrink: 0 }} />
          <Typography variant="body2" noWrap sx={{ flex: 1 }}>{file.name}</Typography>
          {rowEstimate !== null && (
            <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
              ~{rowEstimate} {rowEstimate === 1 ? 'row' : 'rows'}
            </Typography>
          )}
        </Paper>
      )}

      <Button
        variant="contained"
        onClick={handleUpload}
        disabled={!file || loading}
        startIcon={loading ? <CircularProgress size={14} color="inherit" /> : undefined}
        sx={{ alignSelf: 'flex-start' }}
      >
        {loading ? 'Importing...' : 'Upload & Import'}
      </Button>

      {importError && (
        <Alert severity="error" icon={<AlertCircle size={16} />}>
          {importError}
        </Alert>
      )}

      {result && <ResultBox result={result} />}
    </Box>
  )
}

export default function Import() {
  const [activeTab, setActiveTab] = useState<Tab>('json')

  return (
    <Box sx={{ maxWidth: '48rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 600, mb: 3 }}>Import Knowledge</Typography>

      <Tabs
        value={activeTab}
        onChange={(_e, val) => setActiveTab(val as Tab)}
        sx={{ mb: 3, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tab label="Paste JSON" value="json" />
        <Tab label="Upload CSV" value="csv" />
      </Tabs>

      <Box>
        {activeTab === 'json' && <JsonTab />}
        {activeTab === 'csv' && <CsvTab />}
      </Box>
    </Box>
  )
}
