import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createTeam, createAPIKey, storeKnowledge } from '@/lib/api'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Alert from '@mui/material/Alert'
import Select from '@mui/material/Select'
import MenuItem from '@mui/material/MenuItem'
import InputLabel from '@mui/material/InputLabel'
import FormControl from '@mui/material/FormControl'
import Stepper from '@mui/material/Stepper'
import Step from '@mui/material/Step'
import StepLabel from '@mui/material/StepLabel'
import LinearProgress from '@mui/material/LinearProgress'
import IconButton from '@mui/material/IconButton'
import InputAdornment from '@mui/material/InputAdornment'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import PsychologyIcon from '@mui/icons-material/Psychology'

// ---------------------------------------------------------------------------
// Seed data
// ---------------------------------------------------------------------------
const SEED_ENTRIES = [
  {
    title: 'Earnings call transcript prompt template',
    content:
      'Structure your prompt: 1. CONTEXT: ticker, fiscal quarter, consensus EPS. 2. TASK: extract management tone, forward guidance deltas, top 3 risk factors. 3. OUTPUT FORMAT: markdown table with Topic|Signal|Sentiment|Confidence columns.',
    type: 'prompt_template',
    domain: 'financial-analysis',
    tags: ['earnings', 'transcript', 'prompt-engineering'],
  },
  {
    title: 'Avoid recency bias in LLM stock reports',
    content:
      'Provide 5-year CAGR alongside TTM growth. Ask model to compare P/E to 10-year median. Always include "As of [date], with the stock at [price]" in every prompt.',
    type: 'best_practice',
    domain: 'financial-analysis',
    tags: ['bias', 'stock-analysis'],
  },
  {
    title: 'Sector rotation signal checklist',
    content:
      'Verify: RSI cross-sector comparison (90d), Fed funds rate vs sector beta, yield curve slope (2-10 spread), commodity index correlation. Ask LLM to classify sectors as early/mid/late cycle with confidence 1-5.',
    type: 'checklist',
    domain: 'financial-analysis',
    tags: ['sector-rotation', 'macro'],
  },
  {
    title: 'Code review prompt for security vulnerabilities',
    content:
      'Ask the LLM: Review [language] code for SQL injection, XSS, insecure deserialization, hardcoded secrets, error handling leaks. For each finding: describe vulnerability, assign CVSS severity, provide remediation snippet.',
    type: 'prompt_template',
    domain: 'software-engineering',
    tags: ['security', 'code-review'],
  },
  {
    title: 'ADR generation prompt',
    content:
      'State: system name, scale, constraints, options considered. Request: Title, Status, Context, Decision, Consequences, Alternatives, Risks sections. Append: "List top 3 risks and a mitigation for each."',
    type: 'prompt_template',
    domain: 'software-engineering',
    tags: ['architecture', 'adr'],
  },
  {
    title: 'Debugging prompts: give the LLM full context',
    content:
      'Always include: full error + stack trace, relevant code snippet, what you already tried, language version and OS. Anti-pattern: "This function doesn\'t work, fix it."',
    type: 'best_practice',
    domain: 'software-engineering',
    tags: ['debugging', 'context'],
  },
  {
    title: 'EDA summary prompt for tabular datasets',
    content:
      'Paste df.describe() and df.info() output. Ask LLM to identify: columns with >20% missing values, outlier columns by mean/std ratio, likely-categorical numeric columns, recommended feature engineering steps.',
    type: 'prompt_template',
    domain: 'data-science',
    tags: ['eda', 'pandas'],
  },
  {
    title: 'Model evaluation: avoid cherry-picked metrics',
    content:
      'For classification require: accuracy, precision, recall, F1, AUC-ROC, confusion matrix. For regression: MAE, RMSE, R². Always ask: "What would a naive baseline score on these metrics?"',
    type: 'best_practice',
    domain: 'data-science',
    tags: ['model-evaluation', 'metrics'],
  },
  {
    title: 'Reproducibility checklist for ML experiments',
    content:
      'Verify: random seeds set (numpy, torch/tf, train/test split), library versions pinned, dataset version documented, hyperparameter search log saved. Ask LLM to flag anything preventing exact reproduction.',
    type: 'checklist',
    domain: 'data-science',
    tags: ['reproducibility', 'mlops'],
  },
]

const STEP_LABELS = ['Welcome', 'Create Team', 'Create API Key', 'Seed Data']

// ---------------------------------------------------------------------------
// Step 1 — Welcome
// ---------------------------------------------------------------------------
function StepWelcome({ onNext }: { onNext: () => void }) {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
      <Box>
        <Typography variant="h4" sx={{ fontWeight: 700, mb: 1.5 }}>
          Welcome to Tribal Knowledge
        </Typography>
        <Typography color="text.secondary" sx={{ maxWidth: 440, lineHeight: 1.7 }}>
          Tribal Knowledge is a shared memory server for your team. Capture
          prompt templates, best practices, and checklists so every team member
          benefits from collective expertise.
        </Typography>
      </Box>
      <Button variant="contained" size="large" onClick={onNext} sx={{ alignSelf: 'flex-start' }}>
        Get Started
      </Button>
    </Box>
  )
}

// ---------------------------------------------------------------------------
// Step 2 — Create Team
// ---------------------------------------------------------------------------
function StepCreateTeam({ onNext }: { onNext: () => void }) {
  const [name, setName] = useState('')
  const [patterns, setPatterns] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async () => {
    if (!name.trim()) {
      setError('Team name is required.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const domainPatterns = patterns
        .split('\n')
        .map(p => p.trim())
        .filter(Boolean)
      await createTeam(name.trim(), domainPatterns)
      onNext()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create team.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3, maxWidth: 440 }}>
      <Box>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 1 }}>Create a Team</Typography>
        <Typography variant="body2" color="text.secondary">
          Teams let you scope knowledge and manage members. You can add more
          teams later from the Admin panel.
        </Typography>
      </Box>

      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <TextField
          label={<>Team Name <Box component="span" sx={{ color: 'error.main' }}>*</Box></>}
          placeholder="e.g. Research Team"
          value={name}
          onChange={e => setName(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSubmit()}
          fullWidth
        />
        <TextField
          label="Domain Patterns (optional)"
          placeholder="e.g. @yourcompany.com — one per line"
          value={patterns}
          onChange={e => setPatterns(e.target.value)}
          multiline
          minRows={3}
          fullWidth
        />
      </Box>

      {error && <Alert severity="error">{error}</Alert>}

      <Button variant="contained" onClick={handleSubmit} disabled={busy} sx={{ alignSelf: 'flex-start' }}>
        {busy ? 'Creating...' : 'Next'}
      </Button>
    </Box>
  )
}

// ---------------------------------------------------------------------------
// Step 3 — Create API Key
// ---------------------------------------------------------------------------
function StepCreateAPIKey({ onNext }: { onNext: () => void }) {
  const [keyName, setKeyName] = useState('my-first-key')
  const [role, setRole] = useState('admin')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [generatedKey, setGeneratedKey] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const handleGenerate = async () => {
    if (!keyName.trim()) {
      setError('Key name is required.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const result = await createAPIKey(keyName.trim(), role, 'team')
      // raw_key is the plaintext key returned on creation (stored hashed server-side).
      const key: string = result.raw_key ?? ''
      setGeneratedKey(key)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to generate API key.')
    } finally {
      setBusy(false)
    }
  }

  const handleCopy = () => {
    if (!generatedKey) return
    navigator.clipboard.writeText(generatedKey).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3, maxWidth: 440 }}>
      <Box>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 1 }}>Create an API Key</Typography>
        <Typography variant="body2" color="text.secondary">
          API keys authenticate MCP clients and other integrations. Copy this
          key now — it won't be shown again.
        </Typography>
      </Box>

      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <TextField
          label="Key Name"
          placeholder="my-first-key"
          value={keyName}
          onChange={e => setKeyName(e.target.value)}
          disabled={!!generatedKey}
          fullWidth
        />
        <FormControl fullWidth size="small" disabled={!!generatedKey}>
          <InputLabel id="role-label">Role</InputLabel>
          <Select
            labelId="role-label"
            label="Role"
            value={role}
            onChange={e => setRole(e.target.value)}
          >
            <MenuItem value="member">member</MenuItem>
            <MenuItem value="curator">curator</MenuItem>
            <MenuItem value="admin">admin</MenuItem>
          </Select>
        </FormControl>
      </Box>

      {!generatedKey && (
        <>
          {error && <Alert severity="error">{error}</Alert>}
          <Button variant="contained" onClick={handleGenerate} disabled={busy} sx={{ alignSelf: 'flex-start' }}>
            {busy ? 'Generating...' : 'Generate Key'}
          </Button>
        </>
      )}

      {generatedKey && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <Box>
            <Typography variant="body2" sx={{ fontWeight: 500, mb: 1 }}>Your API Key</Typography>
            <TextField
              fullWidth
              value={generatedKey}
              slotProps={{
                htmlInput: { readOnly: true },
                input: {
                  endAdornment: (
                    <InputAdornment position="end">
                      <IconButton onClick={handleCopy} size="small" edge="end">
                        <ContentCopyIcon fontSize="small" />
                      </IconButton>
                    </InputAdornment>
                  ),
                },
              }}
              sx={{
                '& input': {
                  fontFamily: 'monospace',
                  fontSize: 13,
                  color: 'primary.light',
                },
              }}
            />
            {copied && (
              <Typography variant="caption" color="success.main" sx={{ mt: 0.5, display: 'block' }}>
                Copied!
              </Typography>
            )}
          </Box>
          <Alert severity="warning">
            Store this key securely. It cannot be retrieved after leaving this page.
          </Alert>
          <Button variant="contained" onClick={onNext} sx={{ alignSelf: 'flex-start' }}>Next</Button>
        </Box>
      )}
    </Box>
  )
}

// ---------------------------------------------------------------------------
// Step 4 — Seed Example Data
// ---------------------------------------------------------------------------
function StepSeedData({ onFinish }: { onFinish: () => void }) {
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleSeed = async () => {
    setError(null)
    setProgress(0)
    try {
      for (let i = 0; i < SEED_ENTRIES.length; i++) {
        setProgress(i + 1)
        await storeKnowledge(SEED_ENTRIES[i])
      }
      onFinish()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Seeding failed.')
      setProgress(null)
    }
  }

  const isSeeding = progress !== null && progress < SEED_ENTRIES.length
  const progressPct = progress !== null ? Math.round((progress / SEED_ENTRIES.length) * 100) : 0

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3, maxWidth: 440 }}>
      <Box>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 1 }}>Seed Example Knowledge</Typography>
        <Typography variant="body2" color="text.secondary">
          Load 9 example entries across 3 domains (financial analysis, software
          engineering, and data science) to explore the system right away.
        </Typography>
      </Box>

      {progress !== null && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
            <Typography variant="caption" color="text.secondary">
              {progress < SEED_ENTRIES.length
                ? `Seeding ${progress}/${SEED_ENTRIES.length}...`
                : `Done! Seeded ${progress} entries.`}
            </Typography>
            <Typography variant="caption" color="text.secondary">{progressPct}%</Typography>
          </Box>
          <LinearProgress
            variant="determinate"
            value={progressPct}
            sx={{ height: 6, borderRadius: 3 }}
          />
        </Box>
      )}

      {error && <Alert severity="error">{error}</Alert>}

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 3 }}>
        <Button variant="contained" onClick={handleSeed} disabled={isSeeding}>
          {isSeeding ? 'Seeding...' : 'Seed Data'}
        </Button>
        <Button
          variant="text"
          onClick={onFinish}
          disabled={isSeeding}
          sx={{ color: 'text.secondary', textDecoration: 'underline' }}
        >
          Skip
        </Button>
      </Box>
    </Box>
  )
}

// ---------------------------------------------------------------------------
// Main Onboarding component
// ---------------------------------------------------------------------------
export default function Onboarding() {
  const navigate = useNavigate()
  const [step, setStep] = useState(1)
  const TOTAL_STEPS = 4

  const advance = () => setStep(s => s + 1)
  const finish = () => {
    localStorage.setItem('tkm_onboarding_done', '1')
    navigate('/dashboard', { replace: true })
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        bgcolor: 'background.default',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        p: 3,
      }}
    >
      <Box sx={{ width: '100%', maxWidth: 560 }}>
        {/* Brand header */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 5 }}>
          <PsychologyIcon sx={{ color: 'primary.main', fontSize: 28 }} />
          <Typography variant="body1" sx={{ fontWeight: 600 }}>
            Tribal Knowledge
          </Typography>
        </Box>

        {/* Step indicator */}
        <Stepper activeStep={step - 1} sx={{ mb: 5 }}>
          {STEP_LABELS.map((label) => (
            <Step key={label}>
              <StepLabel>{label}</StepLabel>
            </Step>
          ))}
        </Stepper>

        {step === 1 && <StepWelcome onNext={advance} />}
        {step === 2 && <StepCreateTeam onNext={advance} />}
        {step === 3 && <StepCreateAPIKey onNext={advance} />}
        {step === 4 && <StepSeedData onFinish={finish} />}
      </Box>
    </Box>
  )
}
